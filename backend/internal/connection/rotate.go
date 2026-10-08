package connection

import (
	"context"
	"math"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"sama/backend/internal/platform"
	"sama/backend/internal/platform/credentials"
	"sama/backend/internal/workspace"
)

// RequireVersion is the acceptance/revalidation fence for version-scoped work.
// Historical submitted operations retain their separate reconciliation context.
func RequireVersion(ctx context.Context, tx pgx.Tx, scope, id uuid.UUID, version int64) error {
	if err := RequireNewWork(ctx, tx, scope, id); err != nil {
		return err
	}
	var current int64
	if tx.QueryRow(ctx, `SELECT active_credential_version FROM connections WHERE workspace_id=$1 AND id=$2`, scope, id).Scan(&current) != nil {
		return workspace.ErrStore
	}
	if current != version {
		return ErrVersion
	}
	return nil
}

func (s *ValidationService) Rotate(ctx context.Context, actor, scope, id uuid.UUID, input Credential, request string) (int64, error) {
	if s == nil {
		return 0, ErrValidation
	}
	if err := workspace.ConnectionAuthority(ctx, s.pool, actor, scope, id, "manage"); err != nil {
		return 0, err
	}
	var family, account string
	var oldVersion int64
	err := s.pool.QueryRow(ctx, `SELECT api_family,account_identity,active_credential_version FROM connections WHERE workspace_id=$1 AND id=$2`, scope, id).Scan(&family, &account, &oldVersion)
	if err != nil {
		return 0, workspace.ErrStore
	}
	if oldVersion == math.MaxInt64 {
		return 0, ErrVersion
	}
	preview, err := s.Preview(ctx, actor, scope, family, input)
	if err != nil {
		return 0, err
	}
	if preview.AccountIdentity != account {
		return 0, ErrCredential
	}
	version := oldVersion + 1
	envelope, err := EncryptCredential(s.keyring, credentials.Binding{Workspace: scope, Connection: id, Family: family, Version: version}, input)
	if err != nil {
		return 0, ErrCredential
	}
	tx, err := platform.BeginTx(ctx, s.pool, pgx.TxOptions{})
	if err != nil {
		return 0, workspace.ErrStore
	}
	defer tx.Rollback(ctx)
	if _, err = workspace.LockAuthority(ctx, tx, actor, scope); err != nil {
		return 0, err
	}
	var current int64
	if tx.QueryRow(ctx, `SELECT active_credential_version FROM connections WHERE workspace_id=$1 AND id=$2 FOR UPDATE`, scope, id).Scan(&current) != nil {
		return 0, workspace.ErrStore
	}
	if current != oldVersion {
		return 0, ErrVersion
	}
	if err = storeEnvelope(ctx, tx, scope, id, version, envelope); err != nil {
		return 0, err
	}
	verified := []string{}
	for _, capability := range preview.Capabilities {
		if capability.Available {
			verified = append(verified, capability.Action)
		}
	}
	_, err = tx.Exec(ctx, `UPDATE connections SET active_credential_version=$3,permissions_verified_at=$4,verified_capabilities=$5 WHERE workspace_id=$1 AND id=$2`, scope, id, version, preview.VerifiedAt, verified)
	if err != nil {
		return 0, workspace.ErrStore
	}
	if err = credentialAudit(ctx, tx, actor, scope, id, "connection.credential_rotated", request); err != nil {
		return 0, err
	}
	if tx.Commit(ctx) != nil {
		return 0, workspace.ErrStore
	}
	// No credential cache is registered. Immutable versions and RequireVersion
	// prevent an old queued review from acquiring the new credential implicitly.
	return version, nil
}
