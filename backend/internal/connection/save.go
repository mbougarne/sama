package connection

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"sama/backend/internal/audit"
	"sama/backend/internal/inventory"
	"sama/backend/internal/platform"
	"sama/backend/internal/platform/credentials"
	"sama/backend/internal/workspace"
)

var ErrVersion = errors.New("credential version changed")

type SaveInput struct {
	Family     string     `json:"family"`
	Label      string     `json:"label"`
	Credential Credential `json:"credential"`
}

type Saved struct {
	ID     uuid.UUID `json:"id"`
	SyncID uuid.UUID `json:"sync_id"`
}

// Save never accepts preview facts. Preview performs fresh validation on this
// exact value outside the transaction; only its server-derived facts are used.
func (s *ValidationService) Save(ctx context.Context, actor, scope uuid.UUID, input SaveInput, request string) (Saved, error) {
	label := strings.TrimSpace(input.Label)
	if label == "" || len(label) > 100 {
		return Saved{}, workspace.ErrInvalid
	}
	preview, err := s.Preview(ctx, actor, scope, input.Family, input.Credential)
	if err != nil {
		return Saved{}, err
	}
	id, err := uuid.NewV7()
	if err != nil {
		return Saved{}, workspace.ErrStore
	}
	envelope, err := EncryptCredential(s.keyring, credentials.Binding{Workspace: scope, Connection: id, Family: input.Family, Version: 1}, input.Credential)
	if err != nil {
		return Saved{}, ErrCredential
	}
	tx, err := platform.BeginTx(ctx, s.pool, pgx.TxOptions{})
	if err != nil {
		return Saved{}, workspace.ErrStore
	}
	defer tx.Rollback(ctx)
	if _, err = workspace.LockAuthority(ctx, tx, actor, scope); err != nil {
		return Saved{}, err
	}
	verified := []string{}
	for _, capability := range preview.Capabilities {
		if capability.Available {
			verified = append(verified, capability.Action)
		}
	}
	_, err = tx.Exec(ctx, `INSERT INTO connections(id,workspace_id,provider,api_family,account_identity,label,status,active_credential_version,permissions_verified_at,verified_capabilities)
 VALUES($1,$2,$3,$3,$4,$5,'active',1,$6,$7)`, id, scope, input.Family, preview.AccountIdentity, label, preview.VerifiedAt, verified)
	if err != nil {
		return Saved{}, workspace.ErrStore
	}
	if err = storeEnvelope(ctx, tx, scope, id, 1, envelope); err != nil {
		return Saved{}, err
	}
	if err = credentialAudit(ctx, tx, actor, scope, id, "connection.created", request); err != nil {
		return Saved{}, err
	}
	syncID, err := inventory.Initial(ctx, tx, actor, scope, id)
	if err != nil {
		return Saved{}, err
	}
	if tx.Commit(ctx) != nil {
		return Saved{}, workspace.ErrStore
	}
	return Saved{id, syncID}, nil
}

func storeEnvelope(ctx context.Context, tx pgx.Tx, scope, id uuid.UUID, version int64, envelope credentials.Envelope) error {
	_, err := tx.Exec(ctx, `INSERT INTO credential_versions(workspace_id,connection_id,version,ciphertext,nonce,wrapped_data_key,wrapping_nonce,encryption_format,master_key_id)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, scope, id, version, envelope.Ciphertext, envelope.Nonce, envelope.WrappedKey, envelope.WrappingNonce, envelope.Format, envelope.KeyID)
	if err != nil {
		return workspace.ErrStore
	}
	return nil
}

func credentialAudit(ctx context.Context, tx pgx.Tx, actor, scope, id uuid.UUID, kind, request string) error {
	eventID, err := audit.NewID()
	if err != nil {
		return workspace.ErrStore
	}
	if audit.Append(ctx, tx, audit.Event{ID: eventID, Workspace: scope, ActorKind: audit.ActorUser, ActorID: &actor, Type: kind, RequestID: request, Metadata: map[string]string{"connection_id": id.String()}}) != nil {
		return workspace.ErrStore
	}
	return nil
}
