package inventory

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"sama/backend/internal/platform"
	"sama/backend/internal/workspace"
)

var ErrQuota = errors.New("refresh queue quota reached")

// Enqueue joins the caller's transaction. The workspace row lock serializes
// admission across API replicas and membership/grant changes.
func Enqueue(ctx context.Context, tx pgx.Tx, actor, scope, connection uuid.UUID) (uuid.UUID, error) {
	var role, status string
	var actions []string
	var active bool
	var locked uuid.UUID
	err := tx.QueryRow(ctx, `SELECT id FROM workspaces WHERE id=$1 AND closed_at IS NULL FOR UPDATE`, scope).Scan(&locked)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, workspace.ErrNotFound
	}
	if err != nil {
		return uuid.Nil, workspace.ErrStore
	}
	// Read authority only after acquiring the workspace lock. A joined read in
	// the lock statement can retain membership from before a concurrent commit.
	err = tx.QueryRow(ctx, `SELECT m.role FROM memberships m JOIN users u ON u.id=m.user_id
 WHERE m.workspace_id=$1 AND m.user_id=$2 AND u.disabled_at IS NULL`, scope, actor).Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, workspace.ErrNotFound
	}
	if err != nil {
		return uuid.Nil, workspace.ErrStore
	}
	err = tx.QueryRow(ctx, `SELECT c.status,COALESCE(g.actions,'{}'),EXISTS(SELECT 1 FROM credential_versions v
 WHERE v.workspace_id=c.workspace_id AND v.connection_id=c.id AND v.version=c.active_credential_version AND v.revoked_at IS NULL)
 FROM connections c LEFT JOIN connection_grants g ON g.workspace_id=c.workspace_id AND g.connection_id=c.id AND g.user_id=$3
 WHERE c.workspace_id=$1 AND c.id=$2 FOR UPDATE OF c`, scope, connection, actor).Scan(&status, &actions, &active)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, workspace.ErrNotFound
	}
	if err != nil {
		return uuid.Nil, workspace.ErrStore
	}
	read, refresh := false, false
	for _, action := range actions {
		read = read || action == "read"
		refresh = refresh || action == "refresh"
	}
	if !read {
		return uuid.Nil, workspace.ErrNotFound
	}
	if !refresh || !workspace.WithinCeiling(role, "refresh") || status != "active" || !active {
		return uuid.Nil, workspace.ErrDenied
	}
	return enqueueLocked(ctx, tx, scope, connection)
}

func enqueueLocked(ctx context.Context, tx pgx.Tx, scope, connection uuid.UUID) (uuid.UUID, error) {
	var id uuid.UUID
	err := tx.QueryRow(ctx, `SELECT id FROM sync_runs WHERE workspace_id=$1 AND connection_id=$2
 AND resource_type='compute.server' AND scope='global' AND status IN ('pending','running')`, scope, connection).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, workspace.ErrStore
	}
	var count, limit int
	if tx.QueryRow(ctx, `SELECT queue_limit FROM workspaces WHERE id=$1`, scope).Scan(&limit) != nil {
		return uuid.Nil, workspace.ErrStore
	}
	if tx.QueryRow(ctx, `SELECT count(*) FROM jobs WHERE workspace_id=$1 AND state IN ('queued','running')`, scope).Scan(&count) != nil {
		return uuid.Nil, workspace.ErrStore
	}
	if count >= limit {
		return uuid.Nil, ErrQuota
	}
	id, err = uuid.NewV7()
	if err != nil {
		return uuid.Nil, workspace.ErrStore
	}
	jobID, err := uuid.NewV7()
	if err != nil {
		return uuid.Nil, workspace.ErrStore
	}
	_, err = tx.Exec(ctx, `INSERT INTO sync_runs(id,workspace_id,connection_id,resource_type,scope,generation)
 VALUES($1,$2,$3,'compute.server','global',$1)`, id, scope, connection)
	if err != nil {
		return uuid.Nil, workspace.ErrStore
	}
	_, err = tx.Exec(ctx, `INSERT INTO jobs(id,workspace_id,connection_id,type,payload_version,payload,sync_id,deadline)
 VALUES($1,$2,$3,'connection_refresh',2,jsonb_build_object('connection_id',$3::uuid::text,'sync_id',$4::uuid::text),$4,$5)`, jobID, scope, connection, id, time.Now().Add(30*time.Minute))
	if err != nil {
		return uuid.Nil, workspace.ErrStore
	}
	return id, nil
}

func Refresh(ctx context.Context, pool *pgxpool.Pool, actor, scope, connection uuid.UUID) (uuid.UUID, error) {
	tx, err := platform.BeginTx(ctx, pool, pgx.TxOptions{})
	if err != nil {
		return uuid.Nil, workspace.ErrStore
	}
	defer tx.Rollback(ctx)
	id, err := Enqueue(ctx, tx, actor, scope, connection)
	if err != nil {
		return uuid.Nil, err
	}
	if tx.Commit(ctx) != nil {
		return uuid.Nil, workspace.ErrStore
	}
	return id, nil
}

// Initial admits the save transaction's first observation intent under current
// management authority. It creates no grants; workers must recheck dispatch authority.
func Initial(ctx context.Context, tx pgx.Tx, actor, scope, connection uuid.UUID) (uuid.UUID, error) {
	if _, err := workspace.LockAuthority(ctx, tx, actor, scope); err != nil {
		return uuid.Nil, err
	}
	var active bool
	if err := tx.QueryRow(ctx, `SELECT status='active' FROM connections WHERE workspace_id=$1 AND id=$2 FOR UPDATE`, scope, connection).Scan(&active); err != nil {
		return uuid.Nil, workspace.ErrStore
	}
	if !active {
		return uuid.Nil, workspace.ErrDenied
	}
	return enqueueLocked(ctx, tx, scope, connection)
}
