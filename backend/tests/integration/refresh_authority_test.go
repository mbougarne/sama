//go:build integration

package integration

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"sama/backend/internal/inventory"
	"sama/backend/internal/workspace"
)

func TestRefreshRechecksMembershipAfterWorkspaceLock(t *testing.T) {
	pool := identityDatabase(t)
	owner, scope := seedIdentity(t, pool)
	actor, connection := entityID(t), entityID(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	for _, statement := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO users(id,issuer,subject) VALUES($1,'https://issuer.example',$2)`, []any{actor, actor.String()}},
		{`INSERT INTO memberships(workspace_id,user_id,role) VALUES($1,$2,'operator')`, []any{scope, actor}},
		{`INSERT INTO connections(id,workspace_id,provider,api_family,account_identity,label) VALUES($1,$2,'fixture','compute','account','Fixture')`, []any{connection, scope}},
		{`INSERT INTO credential_versions(workspace_id,connection_id,version,ciphertext,nonce,wrapped_data_key,wrapping_nonce,encryption_format,master_key_id) VALUES($1,$2,1,'c','n','k','w',1,'fixture')`, []any{scope, connection}},
		{`UPDATE connections SET status='active',active_credential_version=1 WHERE id=$1`, []any{connection}},
	} {
		if _, err := pool.Exec(ctx, statement.sql, statement.args...); err != nil {
			t.Fatal(err)
		}
	}
	if err := workspace.SetGrant(ctx, pool, owner, scope, connection, actor, []string{"read", "refresh"}, "grant"); err != nil {
		t.Fatal(err)
	}
	change, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer change.Rollback(ctx)
	var locked uuid.UUID
	var blocker int32
	if err := change.QueryRow(ctx, `SELECT id,pg_backend_pid() FROM workspaces WHERE id=$1 FOR UPDATE`, scope).Scan(&locked, &blocker); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		_, err := inventory.Refresh(ctx, pool, actor, scope, connection)
		result <- err
	}()
	// Observe the actual database lock wait before changing authority; a sleep
	// alone would not prove Refresh began its authorization query before commit.
	waitCtx, stopWaiting := context.WithTimeout(ctx, 2*time.Second)
	defer stopWaiting()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var waiting bool
		if err := pool.QueryRow(waitCtx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity
 WHERE datname=current_database() AND state='active' AND wait_event_type='Lock'
 AND $1=ANY(pg_blocking_pids(pid)))`, blocker).Scan(&waiting); err != nil {
			t.Fatal("observe refresh waiting for workspace lock:", err)
		}
		if waiting {
			break
		}
		select {
		case err := <-result:
			t.Fatal("refresh returned before workspace lock was released:", err)
		case <-waitCtx.Done():
			t.Fatal("refresh did not wait for workspace lock")
		case <-ticker.C:
		}
	}
	// Match ChangeMember's locked policy revision and role write, retaining the
	// explicit grant so the viewer ceiling must deny the waiting refresh.
	var revision int64
	if err := change.QueryRow(ctx, `UPDATE workspaces SET policy_version=policy_version+1 WHERE id=$1 RETURNING policy_version`, scope).Scan(&revision); err != nil {
		t.Fatal(err)
	}
	if _, err := change.Exec(ctx, `UPDATE memberships SET role='viewer',version=$3 WHERE workspace_id=$1 AND user_id=$2`, scope, actor, revision); err != nil {
		t.Fatal(err)
	}
	if err := change.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		if !errors.Is(err, workspace.ErrDenied) {
			t.Fatalf("refresh after committed downgrade = %v, want denied", err)
		}
	case <-ctx.Done():
		t.Fatal("refresh did not finish after workspace lock was released")
	}
	var syncs, jobs int
	if err := pool.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM sync_runs WHERE workspace_id=$1),
 (SELECT count(*) FROM jobs WHERE workspace_id=$1)`, scope).Scan(&syncs, &jobs); err != nil {
		t.Fatal(err)
	}
	if syncs != 0 || jobs != 0 {
		t.Fatalf("denied refresh created %d syncs and %d jobs", syncs, jobs)
	}
}
