//go:build integration

package integration

import (
	"context"
	"errors"
	"testing"
	"time"

	"sama/backend/internal/inventory"
	"sama/backend/internal/job"
	"sama/backend/internal/provider"
)

func TestPublicationRetainsCompleteInventoryAndTombstonesOnlyScannedScope(t *testing.T) {
	pool, lease, actor, run := inventoryLease(t)
	ctx := context.Background()
	scanner := inventory.Scanner{Pool: pool, Reader: func(context.Context, provider.Binding) (provider.InventoryReader, error) {
		return scanReader{func(context.Context, string) (provider.InventoryPage, error) {
			return provider.InventoryPage{Data: []provider.Observation{scanObservation("keep"), scanObservation("remove")}}, nil
		}}, nil
	}}
	if err := scanner.Execute(ctx, lease); err != nil {
		t.Fatal(err)
	}
	var stable string
	if err := pool.QueryRow(ctx, `SELECT id::text FROM resources WHERE native_id='keep'`).Scan(&stable); err != nil {
		t.Fatal(err)
	}
	// A separate fully scanned scope must never be inferred missing by global sync.
	separate := entityID(t)
	if _, err := pool.Exec(ctx, `INSERT INTO sync_runs(id,workspace_id,connection_id,resource_type,scope,generation,status) VALUES($1,$2,$3,'compute.server','other',$1,'succeeded')`, separate, lease.Workspace, lease.Connection); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO resources SELECT $1,workspace_id,connection_id,api_family,resource_type,'other','other',native_status,status,name,details_version,details,observed_at,$2,NULL,1 FROM resources LIMIT 1`, entityID(t), separate); err != nil {
		t.Fatal(err)
	}
	refresh := func() job.Lease {
		t.Helper()
		_, err := inventory.Refresh(ctx, pool, actor, lease.Workspace, lease.Connection)
		if err != nil {
			t.Fatal(err)
		}
		next, err := (job.Scheduler{Pool: pool, Now: func() time.Time { return time.Now().Add(time.Second) }}).Claim(ctx, entityID(t))
		if err != nil {
			t.Fatal(err)
		}
		return next
	}
	failed := refresh()
	calls := 0
	scanner.Reader = func(context.Context, provider.Binding) (provider.InventoryReader, error) {
		return scanReader{func(context.Context, string) (provider.InventoryPage, error) {
			calls++
			if calls == 1 {
				return provider.InventoryPage{Data: []provider.Observation{scanObservation("new")}, Next: "last"}, nil
			}
			return provider.InventoryPage{}, errors.New("synthetic-secret-provider-body")
		}}, nil
	}
	if scanner.Execute(ctx, failed) == nil {
		t.Fatal("failed final page succeeded")
	}
	var active int
	if pool.QueryRow(ctx, `SELECT count(*) FROM resources WHERE scope='global' AND tombstoned_at IS NULL`).Scan(&active) != nil || active != 2 {
		t.Fatal("partial refresh changed complete inventory")
	}
	var safe string
	if pool.QueryRow(ctx, `SELECT failure_code FROM sync_runs WHERE id=(SELECT sync_id FROM jobs WHERE id=$1)`, failed.ID).Scan(&safe) != nil || safe != "provider_unavailable" {
		t.Fatal("unsafe failure persisted")
	}
	next := refresh()
	scanner.Reader = func(context.Context, provider.Binding) (provider.InventoryReader, error) {
		return scanReader{func(context.Context, string) (provider.InventoryPage, error) {
			return provider.InventoryPage{Data: []provider.Observation{scanObservation("keep")}}, nil
		}}, nil
	}
	if err := scanner.Execute(ctx, next); err != nil {
		t.Fatal(err)
	}
	var current string
	var removed, other bool
	if pool.QueryRow(ctx, `SELECT id::text FROM resources WHERE native_id='keep'`).Scan(&current) != nil || current != stable {
		t.Fatal("identity changed")
	}
	if pool.QueryRow(ctx, `SELECT tombstoned_at IS NOT NULL FROM resources WHERE native_id='remove'`).Scan(&removed) != nil || !removed {
		t.Fatal("missing resource not tombstoned")
	}
	if pool.QueryRow(ctx, `SELECT tombstoned_at IS NULL FROM resources WHERE native_id='other'`).Scan(&other) != nil || !other {
		t.Fatal("another scope erased")
	}
	if _, err := pool.Exec(ctx, `UPDATE sync_runs SET status='running',scan_complete=true WHERE id=$1`, run); err != nil {
		t.Fatal(err)
	}
	stale := lease
	stale.Token = entityID(t)
	if scanner.Complete(ctx, stale, nil) != job.ErrLease {
		t.Fatal("stale worker published")
	}
	// Reconstitute an older scan with a live lease: generation ordering still
	// prevents replacing the newer publication, independent of lease ownership.
	if _, err := pool.Exec(ctx, `UPDATE jobs SET state='running',lease_owner=$2,lease_token=$3,lease_expires_at=clock_timestamp()+interval '60 seconds' WHERE id=$1`, lease.ID, lease.Owner, lease.Token); err != nil {
		t.Fatal(err)
	}
	if err := scanner.Complete(ctx, lease, nil); err != nil {
		t.Fatal(err)
	}
	if pool.QueryRow(ctx, `SELECT count(*) FROM resources WHERE scope='global' AND tombstoned_at IS NULL`).Scan(&active) != nil || active != 1 {
		t.Fatal("older scan replaced newer")
	}
}

func TestIncompleteAndEmptyGenerationPublication(t *testing.T) {
	pool, lease, _, run := inventoryLease(t)
	scanner := inventory.Scanner{Pool: pool, Reader: func(context.Context, provider.Binding) (provider.InventoryReader, error) {
		return scanReader{func(context.Context, string) (provider.InventoryPage, error) { return provider.InventoryPage{}, nil }}, nil
	}}
	ctx := context.Background()
	if scanner.Complete(ctx, lease, nil) == nil {
		t.Fatal("incomplete generation published")
	}
	if err := scanner.Execute(ctx, lease); err != nil {
		t.Fatal(err)
	}
	var status string
	if pool.QueryRow(ctx, `SELECT status FROM sync_runs WHERE id=$1`, run).Scan(&status) != nil || status != "succeeded" {
		t.Fatal("empty complete scan rejected")
	}
}

func TestPublicationRollsBackWhenTerminalWriteFails(t *testing.T) {
	pool, lease, _, run := inventoryLease(t)
	ctx := context.Background()
	scanner := inventory.Scanner{Pool: pool, Reader: func(context.Context, provider.Binding) (provider.InventoryReader, error) {
		return scanReader{func(context.Context, string) (provider.InventoryPage, error) {
			return provider.InventoryPage{Data: []provider.Observation{scanObservation("atomic")}}, nil
		}}, nil
	}}
	if err := scanner.Scan(ctx, lease); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `CREATE FUNCTION reject_terminal() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.state='succeeded' THEN RAISE EXCEPTION 'synthetic terminal failure'; END IF; RETURN NEW; END $$;
 CREATE TRIGGER reject_terminal BEFORE UPDATE ON jobs FOR EACH ROW EXECUTE FUNCTION reject_terminal()`); err != nil {
		t.Fatal(err)
	}
	if scanner.Complete(ctx, lease, nil) == nil {
		t.Fatal("terminal failure ignored")
	}
	var count int
	var status string
	if pool.QueryRow(ctx, `SELECT count(*) FROM resources`).Scan(&count) != nil || count != 0 {
		t.Fatal("publication escaped rollback")
	}
	if pool.QueryRow(ctx, `SELECT status FROM sync_runs WHERE id=$1`, run).Scan(&status) != nil || status != "running" {
		t.Fatal("sync escaped rollback")
	}
	if _, err := pool.Exec(ctx, `DROP TRIGGER reject_terminal ON jobs`); err != nil {
		t.Fatal(err)
	}
	if err := scanner.Complete(ctx, lease, nil); err != nil {
		t.Fatal(err)
	}
	if pool.QueryRow(ctx, `SELECT count(*) FROM resources`).Scan(&count) != nil || count != 1 {
		t.Fatal("retry did not publish")
	}
}
