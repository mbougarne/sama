package connection

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"sama/backend/internal/audit"
	"sama/backend/internal/platform"
	"sama/backend/internal/workspace"
)

var ErrDisabled = errors.New("connection cannot accept new work")

// RequireNewWork is the shared acceptance/dispatch guard. Submitted operations
// must use their separate reconciliation path; this guard never deletes history.
// Retain the row lock through the caller's acceptance or dispatch transaction.
func RequireNewWork(ctx context.Context, tx pgx.Tx, scope, id uuid.UUID) error {
	var usable bool
	err := tx.QueryRow(ctx, `SELECT c.status='active' AND v.revoked_at IS NULL AND v.version IS NOT NULL
        FROM connections c LEFT JOIN credential_versions v ON (v.workspace_id,v.connection_id,v.version)=(c.workspace_id,c.id,c.active_credential_version)
        WHERE c.workspace_id=$1 AND c.id=$2 FOR UPDATE OF c`, scope, id).Scan(&usable)
	if errors.Is(err, pgx.ErrNoRows) {
		return workspace.ErrNotFound
	}
	if err != nil {
		return workspace.ErrStore
	}
	if !usable {
		return ErrDisabled
	}
	return nil
}

func Disable(ctx context.Context, pool *pgxpool.Pool, actor, scope, id uuid.UUID, request string) error {
	tx, err := platform.BeginTx(ctx, pool, pgx.TxOptions{})
	if err != nil {
		return workspace.ErrStore
	}
	defer tx.Rollback(ctx)
	if _, err := workspace.LockAuthority(ctx, tx, actor, scope); err != nil {
		return err
	}
	var status string
	err = tx.QueryRow(ctx, `SELECT status FROM connections WHERE workspace_id=$1 AND id=$2 FOR UPDATE`, scope, id).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return workspace.ErrNotFound
	}
	if err != nil {
		return workspace.ErrStore
	}
	if status == "disabled" {
		return nil
	}
	if _, err := tx.Exec(ctx, `UPDATE connections SET status='disabled' WHERE workspace_id=$1 AND id=$2`, scope, id); err != nil {
		return workspace.ErrStore
	}
	// Only the unsent refresh records this storage slice can represent are cancelled.
	// Future submitted mutations must retain reconciliation jobs and visibility.
	if _, err := tx.Exec(ctx, `UPDATE jobs SET state='cancelled' WHERE workspace_id=$1 AND connection_id=$2 AND state='queued'`, scope, id); err != nil {
		return workspace.ErrStore
	}
	eventID, err := audit.NewID()
	if err != nil {
		return workspace.ErrStore
	}
	if audit.Append(ctx, tx, audit.Event{ID: eventID, Workspace: scope, ActorKind: audit.ActorUser, ActorID: &actor, Type: "connection.disabled", RequestID: request, Metadata: map[string]string{"connection_id": id.String()}}) != nil {
		return workspace.ErrStore
	}
	if tx.Commit(ctx) != nil {
		return workspace.ErrStore
	}
	return nil
}
