//go:build integration

package integration

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"sama/backend/internal/inventory"
	"sama/backend/internal/workspace"
)

func TestRefreshCoalescesAcrossReplicasAndHonorsQuota(t *testing.T) {
	pool := identityDatabase(t)
	actor, scope := seedIdentity(t, pool)
	ctx := context.Background()
	seed := func() uuid.UUID {
		t.Helper()
		id := entityID(t)
		if _, err := pool.Exec(ctx, `INSERT INTO connections(id,workspace_id,provider,api_family,account_identity,label) VALUES($1,$2,'fixture','compute','account','Fixture')`, id, scope); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO credential_versions(workspace_id,connection_id,version,ciphertext,nonce,wrapped_data_key,wrapping_nonce,encryption_format,master_key_id) VALUES($1,$2,1,'c','n','k','w',1,'fixture')`, scope, id); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `UPDATE connections SET status='active',active_credential_version=1 WHERE id=$1`, id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	first := seed()
	second := seed()
	if _, err := inventory.Refresh(ctx, pool, actor, scope, first); err != workspace.ErrNotFound {
		t.Fatal("implicit owner refresh", err)
	}
	for _, id := range []uuid.UUID{first, second} {
		if err := workspace.SetGrant(ctx, pool, actor, scope, id, actor, []string{"read", "refresh"}, "grant"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, `UPDATE workspaces SET queue_limit=1 WHERE id=$1`, scope); err != nil {
		t.Fatal(err)
	}
	results := make(chan uuid.UUID, 8)
	failures := make(chan error, 8)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id, err := inventory.Refresh(ctx, pool, actor, scope, first)
			if err != nil {
				failures <- err
			} else {
				results <- id
			}
		}()
	}
	wg.Wait()
	close(results)
	if len(failures) != 0 {
		t.Fatal(<-failures)
	}
	var expected uuid.UUID
	for id := range results {
		if expected == uuid.Nil {
			expected = id
		}
		if id != expected {
			t.Fatal("duplicate refresh scope")
		}
	}
	if _, err := inventory.Refresh(ctx, pool, actor, scope, second); !errors.Is(err, inventory.ErrQuota) {
		t.Fatal("quota ignored", err)
	}
	var count int
	if pool.QueryRow(ctx, `SELECT count(*) FROM jobs WHERE workspace_id=$1`, scope).Scan(&count) != nil || count != 1 {
		t.Fatal("duplicate jobs")
	}
	if _, err := pool.Exec(ctx, `UPDATE connections SET status='disabled' WHERE id=$1`, first); err != nil {
		t.Fatal(err)
	}
	if _, err := inventory.Refresh(ctx, pool, actor, scope, first); err != workspace.ErrDenied {
		t.Fatal("disabled refresh", err)
	}
}

func TestInventoryUpsertPreservesIdentityAndSafeDetails(t *testing.T) {
	pool := identityDatabase(t)
	_, scope := seedIdentity(t, pool)
	ctx := context.Background()
	id, run := entityID(t), entityID(t)
	if _, err := pool.Exec(ctx, `INSERT INTO connections(id,workspace_id,provider,api_family,account_identity,label) VALUES($1,$2,'fixture','compute','account','Fixture')`, id, scope); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO sync_runs(id,workspace_id,connection_id,resource_type,scope,generation) VALUES($1,$2,$3,'compute.server','global',$1)`, run, scope, id); err != nil {
		t.Fatal(err)
	}
	observation := inventory.Observation{ID: entityID(t), Workspace: scope, Connection: id, Family: "compute", Scope: "global", NativeID: "server-1", NativeStatus: "running", Status: "running", Name: "Fixture", Generation: run, ObservedAt: time.Now(), Details: inventory.Details{Region: "fixture-region"}}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	stable, err := inventory.Upsert(ctx, tx, observation)
	if err != nil {
		t.Fatal(err)
	}
	observation.ID = entityID(t)
	observation.Name = "Updated"
	repeated, err := inventory.Upsert(ctx, tx, observation)
	if err != nil || stable != repeated {
		t.Fatal("identity changed", err)
	}
	if tx.Commit(ctx) != nil {
		t.Fatal("commit")
	}
	for _, sql := range []string{`UPDATE resources SET details='{"password":"synthetic-secret"}'`, `UPDATE resources SET details=jsonb_build_object('region',repeat('x',65536))`, `UPDATE resources SET details_version=2`} {
		if _, err := pool.Exec(ctx, sql); err == nil {
			t.Fatal("unsafe details accepted")
		}
	}
	tx, err = pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	observation.Generation = entityID(t)
	if _, err = inventory.Upsert(ctx, tx, observation); err != inventory.ErrObservation {
		t.Fatal("unknown generation accepted", err)
	}
}
