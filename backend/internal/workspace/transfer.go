package workspace

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"sama/backend/internal/platform"
)

// Transfer grants a current member ownership and optionally demotes the actor.
// Both expected versions and current authority are checked under the same lock
// used by removals. Two member audit events identify the complete change.
func Transfer(ctx context.Context, pool *pgxpool.Pool, actor, scope, target uuid.UUID, actorVersion, targetVersion int64, demote bool, request string) error {
	if actor == target || actorVersion < 1 || targetVersion < 1 {
		return ErrInvalid
	}
	tx, err := platform.BeginTx(ctx, pool, pgx.TxOptions{})
	if err != nil {
		return ErrStore
	}
	defer tx.Rollback(ctx)
	caller, err := LockAuthority(ctx, tx, actor, scope)
	if err != nil {
		return err
	}
	if caller.Role != "owner" {
		return ErrDenied
	}
	member, err := memberInTx(ctx, tx, scope, target)
	if err != nil {
		return err
	}
	if caller.Version != actorVersion || member.Version != targetVersion {
		return ErrConflict
	}
	var revision int64
	if err := tx.QueryRow(ctx, `UPDATE workspaces SET policy_version=policy_version+1 WHERE id=$1 RETURNING policy_version`, scope).Scan(&revision); err != nil {
		return ErrStore
	}
	if _, err := tx.Exec(ctx, `UPDATE memberships SET role='owner',version=$3 WHERE workspace_id=$1 AND user_id=$2`, scope, target, revision); err != nil {
		return ErrStore
	}
	if err := memberAudit(ctx, tx, actor, scope, target, "owner", request); err != nil {
		return err
	}
	if demote {
		if _, err := tx.Exec(ctx, `UPDATE memberships SET role='admin',version=$3 WHERE workspace_id=$1 AND user_id=$2`, scope, actor, revision); err != nil {
			return ErrStore
		}
		if err := memberAudit(ctx, tx, actor, scope, actor, "admin", request); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return ErrStore
	}
	return nil
}
