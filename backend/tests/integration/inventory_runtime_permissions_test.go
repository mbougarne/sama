//go:build integration

package integration

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestRuntimeRoleCanUseGenerationCountersWithoutDDLOrEventRewrites(t *testing.T) {
	pool, lease, actor, _ := operationFixture(t)
	ctx := context.Background()
	role := pgx.Identifier{"sama_runtime_" + entityID(t).String()}.Sanitize()
	if _, err := pool.Exec(ctx, `CREATE ROLE `+role+` NOLOGIN`); err != nil {
		t.Fatal("fixture runtime role")
	}
	source, err := os.ReadFile(filepath.Join("..", "..", "deploy", "postgres", "grants.sql"))
	if err != nil {
		t.Fatal("read tracked grants")
	}
	grants := strings.NewReplacer(`:"runtime_role"`, role, `:"migration_role"`, pgx.Identifier{"postgres"}.Sanitize(), `:"database_name"`, pgx.Identifier{pool.Config().ConnConfig.Database}.Sanitize()).Replace(string(source))
	if _, err = pool.Exec(ctx, grants); err != nil {
		t.Fatal("apply tracked fixture grants", err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SET LOCAL ROLE `+role); err != nil {
		t.Fatal("set fixture role")
	}
	run := entityID(t)
	if _, err = tx.Exec(ctx, `INSERT INTO sync_runs(id,workspace_id,connection_id,resource_type,scope,generation,actor_id) VALUES($1,$2,$3,'compute.server','runtime',$1,$4)`, run, lease.Workspace, lease.Connection, actor); err != nil {
		t.Fatal("runtime identity counter unavailable", err)
	}
	if tx.Commit(ctx) != nil {
		t.Fatal("runtime insert failed")
	}
	tx, err = pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SET LOCAL ROLE `+role); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `UPDATE operation_events SET to_state='failed'`); err == nil {
		t.Fatal("runtime rewrote append-only history")
	}
}
