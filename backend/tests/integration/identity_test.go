//go:build integration

package integration

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"sama/backend/internal/identifier"
	"sama/backend/internal/platform"
	"sama/backend/migrations"
)

func identityDatabase(t *testing.T) *pgxpool.Pool {
	t.Helper()
	fixture := newPostgresFixture(t)
	database := fixture.createDatabase(t)
	pool, err := platform.OpenPool(context.Background(), platform.DatabaseConfig{URL: database.url})
	if err != nil {
		t.Fatal("open identity database")
	}
	t.Cleanup(pool.Close)
	if err := platform.ApplyMigrations(context.Background(), pool, migrations.Files, integrationMigrationQueryTimeout); err != nil {
		t.Fatal(err)
	}
	return pool
}

func entityID(t *testing.T) uuid.UUID {
	t.Helper()
	id, err := identifier.New()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func seedIdentity(t *testing.T, pool *pgxpool.Pool) (uuid.UUID, uuid.UUID) {
	t.Helper()
	user, workspace := entityID(t), entityID(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `INSERT INTO users (id, issuer, subject) VALUES ($1, 'https://issuer.example', $2)`, user, user.String()); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO workspaces (id, name) VALUES ($1, 'Test workspace')`, workspace); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO memberships (workspace_id, user_id, role) VALUES ($1, $2, 'owner')`, workspace, user); err != nil {
		t.Fatal(err)
	}
	return user, workspace
}

func TestIdentityConstraintsAndHistoricalActors(t *testing.T) {
	pool := identityDatabase(t)
	user, workspace := seedIdentity(t, pool)
	ctx := context.Background()
	for _, probe := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO users (id, issuer, subject) VALUES ($1, 'https://issuer.example', $2)`, []any{entityID(t), user.String()}},
		{`INSERT INTO memberships VALUES ($1, $2, 'owner', 1)`, []any{entityID(t), user}},
		{`INSERT INTO memberships VALUES ($1, $2, 'owner', 1)`, []any{workspace, entityID(t)}},
		{`UPDATE memberships SET role = 'superuser' WHERE workspace_id = $1`, []any{workspace}},
		{`UPDATE memberships SET version = 0 WHERE workspace_id = $1`, []any{workspace}},
	} {
		if _, err := pool.Exec(ctx, probe.sql, probe.args...); err == nil {
			t.Fatalf("invalid identity relationship accepted: %s", probe.sql)
		}
	}
	if _, err := pool.Exec(ctx, `INSERT INTO audit_events (id, workspace_id, actor_kind, actor_id, event_type, request_id) VALUES ($1, $2, 'user', $3, 'workspace.created', 'identity-test')`, entityID(t), workspace, user); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM memberships WHERE workspace_id = $1 AND user_id = $2`, workspace, user); err != nil {
		t.Fatal("membership removal failed")
	}
	for _, probe := range []struct {
		sql string
		id  uuid.UUID
	}{
		{`DELETE FROM users WHERE id = $1`, user},
		{`DELETE FROM workspaces WHERE id = $1`, workspace},
	} {
		if _, err := pool.Exec(ctx, probe.sql, probe.id); err == nil {
			t.Fatal("historical audit reference was deleted")
		}
	}
	var actor uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT actor_id FROM audit_events WHERE request_id = 'identity-test'`).Scan(&actor); err != nil || actor != user {
		t.Fatal("membership removal lost historical actor")
	}
}
