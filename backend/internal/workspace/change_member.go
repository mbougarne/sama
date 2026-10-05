package workspace

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"sama/backend/internal/audit"
	"sama/backend/internal/platform"
)

var ErrConflict = errors.New("membership version or final owner conflict")

// LockAuthority serializes membership writers on the workspace before reading
// current authority. Callers must retain this transaction through audit/commit.
func LockAuthority(ctx context.Context, tx pgx.Tx, actor, scope uuid.UUID) (Membership, error) {
	var id uuid.UUID
	err := tx.QueryRow(ctx, `SELECT id FROM workspaces WHERE id=$1 AND closed_at IS NULL FOR UPDATE`, scope).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return Membership{}, ErrNotFound
	}
	if err != nil {
		return Membership{}, ErrStore
	}
	m, err := memberInTx(ctx, tx, scope, actor)
	if err != nil {
		return m, err
	}
	if !Allows(m.Role, "admin") {
		return m, ErrDenied
	}
	return m, nil
}
func memberInTx(ctx context.Context, tx pgx.Tx, scope, user uuid.UUID) (Membership, error) {
	m := Membership{WorkspaceID: scope, UserID: user}
	err := tx.QueryRow(ctx, `SELECT m.role,m.version FROM memberships m JOIN users u ON u.id=m.user_id WHERE m.workspace_id=$1 AND m.user_id=$2 AND u.disabled_at IS NULL`, scope, user).Scan(&m.Role, &m.Version)
	if errors.Is(err, pgx.ErrNoRows) {
		return m, ErrNotFound
	}
	if err != nil {
		return m, ErrStore
	}
	return m, nil
}
func CanAssign(actor, role string) bool {
	return Allows(role, "viewer") && (actor == "owner" || actor == "admin" && (role == "viewer" || role == "operator"))
}
func memberAudit(ctx context.Context, tx pgx.Tx, actor, scope, target uuid.UUID, role, request string) error {
	id, err := audit.NewID()
	if err != nil {
		return ErrStore
	}
	metadata := map[string]string{"member_id": target.String()}
	if role != "" {
		metadata["role"] = role
	}
	if audit.Append(ctx, tx, audit.Event{ID: id, Workspace: scope, ActorKind: audit.ActorUser, ActorID: &actor, Type: "membership.changed", RequestID: request, Metadata: metadata}) != nil {
		return ErrStore
	}
	return nil
}

// Version zero creates an absent membership for an already admitted identity.
// Empty role removes it. Existing membership changes require the exact version.
func ChangeMember(ctx context.Context, pool *pgxpool.Pool, actor, scope, target uuid.UUID, role string, version int64, request string) error {
	if version < 0 || role != "" && !Allows(role, "viewer") {
		return ErrInvalid
	}
	tx, err := platform.BeginTx(ctx, pool, pgx.TxOptions{})
	if err != nil {
		return ErrStore
	}
	defer tx.Rollback(ctx)
	current, err := LockAuthority(ctx, tx, actor, scope)
	if err != nil {
		return err
	}
	previous, err := memberInTx(ctx, tx, scope, target)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	if !CanAssign(current.Role, role) && role != "" || previous.Role != "" && !CanAssign(current.Role, previous.Role) {
		return ErrDenied
	}
	if previous.Version != version || previous.Version == 0 && role == "" {
		return ErrConflict
	}
	if previous.Role == "owner" && role != "owner" {
		var owners int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM memberships m JOIN users u ON u.id=m.user_id WHERE m.workspace_id=$1 AND m.role='owner' AND u.disabled_at IS NULL`, scope).Scan(&owners); err != nil {
			return ErrStore
		}
		if owners <= 1 {
			return ErrConflict
		}
	}
	// Workspace revision survives deletion/reassignment, preventing stale-version ABA.
	var revision int64
	if err := tx.QueryRow(ctx, `UPDATE workspaces SET policy_version=policy_version+1 WHERE id=$1 RETURNING policy_version`, scope).Scan(&revision); err != nil {
		return ErrStore
	}
	if version == 0 {
		var admitted bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users u JOIN memberships m ON m.user_id=u.id JOIN workspaces w ON w.id=m.workspace_id WHERE u.id=$1 AND u.disabled_at IS NULL AND w.closed_at IS NULL)`, target).Scan(&admitted); err != nil {
			return ErrStore
		}
		if !admitted {
			return ErrNotFound
		}
		_, err = tx.Exec(ctx, `INSERT INTO memberships(workspace_id,user_id,role,version) VALUES($1,$2,$3,$4)`, scope, target, role, revision)
	} else if role == "" {
		_, err = tx.Exec(ctx, `DELETE FROM memberships WHERE workspace_id=$1 AND user_id=$2`, scope, target)
	} else {
		_, err = tx.Exec(ctx, `UPDATE memberships SET role=$3,version=$4 WHERE workspace_id=$1 AND user_id=$2`, scope, target, role, revision)
	}
	if err != nil {
		return ErrStore
	}
	if err := invalidateElevation(ctx, tx, target, previous.Role, role); err != nil {
		return err
	}
	if err := memberAudit(ctx, tx, actor, scope, target, role, request); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return ErrStore
	}
	return nil
}

// Privilege elevation invalidates existing sessions; the next login rotates the
// browser credential before it can exercise newly granted authority.
func invalidateElevation(ctx context.Context, tx pgx.Tx, user uuid.UUID, previous, next string) error {
	if next != "" && !Allows(previous, next) {
		if _, err := tx.Exec(ctx, `DELETE FROM sessions WHERE user_id=$1`, user); err != nil {
			return ErrStore
		}
	}
	return nil
}
