package workspace

import (
	"context"
	"errors"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"sama/backend/internal/audit"
	"sama/backend/internal/platform"
)

// Grant classes are independent: read never implies refresh or mutation.
func WithinCeiling(role, action string) bool {
	switch action {
	case "read":
		return Allows(role, "viewer")
	case "refresh", "operate", "high_impact":
		return Allows(role, "operator")
	default:
		return false
	}
}

// ConnectionAuthority reads current membership, status and grant in one snapshot.
// Management authority does not confer provider action authority, even for owners.
func ConnectionAuthority(ctx context.Context, pool *pgxpool.Pool, actor, scope, connection uuid.UUID, action string) error {
	var role, status string
	var actions []string
	err := pool.QueryRow(ctx, `SELECT m.role,c.status,COALESCE(g.actions,'{}')
        FROM memberships m JOIN users u ON u.id=m.user_id AND u.disabled_at IS NULL
        JOIN workspaces w ON w.id=m.workspace_id AND w.closed_at IS NULL
        JOIN connections c ON c.workspace_id=w.id
        LEFT JOIN connection_grants g ON (g.workspace_id,g.connection_id,g.user_id)=(w.id,c.id,m.user_id)
        WHERE w.id=$1 AND m.user_id=$2 AND c.id=$3`, scope, actor, connection).Scan(&role, &status, &actions)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return ErrStore
	}
	if action == "manage" {
		if Allows(role, "admin") {
			return nil
		}
		return ErrDenied
	}
	if !slices.Contains(actions, "read") {
		return ErrNotFound
	}
	if !WithinCeiling(role, action) || !slices.Contains(actions, action) || action != "read" && status != "active" {
		return ErrDenied
	}
	return nil
}

// ReadGrant returns the caller's grant, including the empty deny-by-default state.
func ReadGrant(ctx context.Context, pool *pgxpool.Pool, actor, scope, connection uuid.UUID) ([]string, error) {
	var actions []string
	err := pool.QueryRow(ctx, `SELECT COALESCE(g.actions,'{}') FROM memberships m
        JOIN users u ON u.id=m.user_id AND u.disabled_at IS NULL
        JOIN workspaces w ON w.id=m.workspace_id AND w.closed_at IS NULL
        JOIN connections c ON c.workspace_id=w.id
        LEFT JOIN connection_grants g ON (g.workspace_id,g.connection_id,g.user_id)=(w.id,c.id,m.user_id)
        WHERE w.id=$1 AND m.user_id=$2 AND c.id=$3
        AND (m.role IN ('admin','owner') OR 'read'=ANY(g.actions))`, scope, actor, connection).Scan(&actions)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, ErrStore
	}
	return actions, nil
}

func grantAudit(ctx context.Context, tx pgx.Tx, actor, scope, connection, target uuid.UUID, kind, request string) error {
	id, err := audit.NewID()
	if err != nil {
		return ErrStore
	}
	if audit.Append(ctx, tx, audit.Event{ID: id, Workspace: scope, ActorKind: audit.ActorUser, ActorID: &actor, Type: kind, RequestID: request, Metadata: map[string]string{"connection_id": connection.String(), "member_id": target.String()}}) != nil {
		return ErrStore
	}
	return nil
}

// SetGrant serializes against membership changes and denies admin self-escalation.
// An admin can delegate only action classes it currently holds on this connection.
func SetGrant(ctx context.Context, pool *pgxpool.Pool, actor, scope, connection, target uuid.UUID, actions []string, request string) error {
	if len(actions) > 4 {
		return ErrInvalid
	}
	seen := map[string]bool{}
	for _, action := range actions {
		if !WithinCeiling("owner", action) || seen[action] {
			return ErrInvalid
		}
		seen[action] = true
	}
	if len(actions) > 0 && !seen["read"] {
		return ErrInvalid
	}
	if actions == nil {
		actions = []string{}
	}
	tx, err := platform.BeginTx(ctx, pool, pgx.TxOptions{})
	if err != nil {
		return ErrStore
	}
	defer tx.Rollback(ctx)
	deny := func(cause error) error {
		if err := grantAudit(ctx, tx, actor, scope, connection, target, "connection.grants_denied", request); err != nil {
			return err
		}
		if tx.Commit(ctx) != nil {
			return ErrStore
		}
		return cause
	}
	current, err := LockAuthority(ctx, tx, actor, scope)
	if err != nil {
		if errors.Is(err, ErrDenied) {
			return deny(err)
		}
		return err
	}
	var status string
	err = tx.QueryRow(ctx, `SELECT status FROM connections WHERE workspace_id=$1 AND id=$2 FOR UPDATE`, scope, connection).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return deny(ErrNotFound)
	}
	if err != nil {
		return ErrStore
	}
	member, err := memberInTx(ctx, tx, scope, target)
	if errors.Is(err, ErrNotFound) {
		return deny(err)
	}
	if err != nil {
		return err
	}
	denied := !CanAssign(current.Role, member.Role)
	var own []string
	if current.Role == "admin" {
		err = tx.QueryRow(ctx, `SELECT actions FROM connection_grants WHERE workspace_id=$1 AND connection_id=$2 AND user_id=$3`, scope, connection, actor).Scan(&own)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return ErrStore
		}
	}
	for _, action := range actions {
		if !WithinCeiling(member.Role, action) || current.Role == "admin" && !slices.Contains(own, action) {
			denied = true
		}
	}
	kind := "connection.grants_changed"
	if denied {
		kind = "connection.grants_denied"
	}
	if err := grantAudit(ctx, tx, actor, scope, connection, target, kind, request); err != nil {
		return err
	}
	if !denied {
		_, err = tx.Exec(ctx, `INSERT INTO connection_grants(workspace_id,connection_id,user_id,actions) VALUES($1,$2,$3,$4)
            ON CONFLICT(workspace_id,connection_id,user_id) DO UPDATE SET actions=EXCLUDED.actions`, scope, connection, target, actions)
		if err != nil {
			return ErrStore
		}
		if _, err = tx.Exec(ctx, `UPDATE workspaces SET policy_version=policy_version+1 WHERE id=$1`, scope); err != nil {
			return ErrStore
		}
	}
	if tx.Commit(ctx) != nil {
		return ErrStore
	}
	if denied {
		return ErrDenied
	}
	return nil
}
