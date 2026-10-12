//go:build integration

package integration

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"sama/backend/internal/inventory"
	"sama/backend/internal/job"
	"sama/backend/internal/provider"
	"sama/backend/internal/workspace"
)

type scanReader struct {
	page func(context.Context, string) (provider.InventoryPage, error)
}

func (r scanReader) Read(context.Context, provider.Binding, string) (provider.Observation, error) {
	return provider.Observation{}, errors.New("unused")
}
func (r scanReader) Page(ctx context.Context, _ provider.Binding, cursor string) (provider.InventoryPage, error) {
	return r.page(ctx, cursor)
}

func inventoryLease(t *testing.T) (*pgxpool.Pool, job.Lease, uuid.UUID, uuid.UUID) {
	return inventoryLeaseForFamily(t, "compute")
}

func inventoryLeaseForFamily(t *testing.T, family string) (*pgxpool.Pool, job.Lease, uuid.UUID, uuid.UUID) {
	t.Helper()
	pool := identityDatabase(t)
	actor, scope := seedIdentity(t, pool)
	id := entityID(t)
	ctx := context.Background()
	for _, stmt := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO connections(id,workspace_id,provider,api_family,account_identity,label) VALUES($1,$2,'fixture',$3,'account','Fixture')`, []any{id, scope, family}},
		{`INSERT INTO credential_versions(workspace_id,connection_id,version,ciphertext,nonce,wrapped_data_key,wrapping_nonce,encryption_format,master_key_id) VALUES($1,$2,1,'c','n','k','w',1,'fixture')`, []any{scope, id}},
		{`UPDATE connections SET active_credential_version=1,status='active' WHERE id=$1`, []any{id}},
	} {
		if _, err := pool.Exec(ctx, stmt.sql, stmt.args...); err != nil {
			t.Fatal(err)
		}
	}
	if err := workspace.SetGrant(ctx, pool, actor, scope, id, actor, []string{"read", "refresh"}, "scan-grant"); err != nil {
		t.Fatal(err)
	}
	run, err := inventory.Refresh(ctx, pool, actor, scope, id)
	if err != nil {
		t.Fatal(err)
	}
	lease, err := (job.Scheduler{Pool: pool, Now: func() time.Time { return time.Now().Add(time.Second) }}).Claim(ctx, entityID(t))
	if err != nil {
		t.Fatal(err)
	}
	return pool, lease, actor, run
}

func scanObservation(id string) provider.Observation {
	return provider.Observation{NativeID: id, NativeStatus: "unrecognized", Status: "unknown", Name: "Safe", ObservedAt: time.Now().UTC(), Details: provider.ServerDetails{Region: "region", MachineType: "small"}}
}

func TestScanStagesPagesWithoutPublishingAndBoundsPagination(t *testing.T) {
	pool, lease, _, run := inventoryLease(t)
	ctx := context.Background()
	calls := 0
	scanner := inventory.Scanner{Pool: pool, Reader: func(_ context.Context, b provider.Binding) (provider.InventoryReader, error) {
		if b.Connection() != lease.Connection || b.Workspace() != lease.Workspace || b.Version() != 1 {
			t.Fatal("binding escaped")
		}
		return scanReader{func(_ context.Context, cursor string) (provider.InventoryPage, error) {
			calls++
			if cursor == "" {
				return provider.InventoryPage{Data: []provider.Observation{scanObservation("first")}, Next: "second"}, nil
			}
			return provider.InventoryPage{}, errors.New("synthetic upstream secret")
		}}, nil
	}}
	if scanner.Scan(ctx, lease) == nil || calls != 2 {
		t.Fatal("partial scan succeeded")
	}
	var public, staged, pages int
	if pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM resources),(SELECT count(*) FROM inventory_staging WHERE generation=$1),(SELECT page_count FROM sync_runs WHERE id=$1)`, run).Scan(&public, &staged, &pages) != nil || public != 0 || staged != 1 || pages != 1 {
		t.Fatal("partial visibility", public, staged, pages)
	}
	scanner.Reader = func(context.Context, provider.Binding) (provider.InventoryReader, error) {
		return scanReader{func(context.Context, string) (provider.InventoryPage, error) {
			return provider.InventoryPage{Next: "repeat"}, nil
		}}, nil
	}
	if err := scanner.Scan(ctx, lease); err == nil || err.Error() != "sync_limit_reached" {
		t.Fatal("cursor loop", err)
	}
	scanner.Reader = func(context.Context, provider.Binding) (provider.InventoryReader, error) {
		return scanReader{func(context.Context, string) (provider.InventoryPage, error) {
			return provider.InventoryPage{Data: []provider.Observation{scanObservation("last")}}, nil
		}}, nil
	}
	if err := scanner.Scan(ctx, lease); err != nil {
		t.Fatal(err)
	}
	var complete bool
	var status, native, region string
	if pool.QueryRow(ctx, `SELECT scan_complete FROM sync_runs WHERE id=$1`, run).Scan(&complete) != nil || !complete {
		t.Fatal("scan not completed")
	}
	if pool.QueryRow(ctx, `SELECT status,native_status,details->>'region' FROM inventory_staging WHERE generation=(SELECT generation FROM sync_runs WHERE id=$1)`, run).Scan(&status, &native, &region) != nil || status != "unknown" || native != "unrecognized" || region != "region" {
		t.Fatal("normalization lost safe details")
	}
}

func TestScanRechecksGrantAndLeaseBeforeProviderTraffic(t *testing.T) {
	pool, lease, actor, _ := inventoryLease(t)
	calls := 0
	scanner := inventory.Scanner{Pool: pool, Reader: func(context.Context, provider.Binding) (provider.InventoryReader, error) {
		return scanReader{func(context.Context, string) (provider.InventoryPage, error) {
			calls++
			return provider.InventoryPage{}, nil
		}}, nil
	}}
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `DELETE FROM connection_grants WHERE user_id=$1`, actor); err != nil {
		t.Fatal(err)
	}
	if scanner.Scan(ctx, lease) == nil || calls != 0 {
		t.Fatal("revoked actor called provider")
	}
	if _, err := pool.Exec(ctx, `UPDATE jobs SET lease_expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`, lease.ID); err != nil {
		t.Fatal(err)
	}
	if scanner.Scan(ctx, lease) == nil || calls != 0 {
		t.Fatal("expired lease called provider")
	}
}

func TestScanRejectsOversizedPagesAndHonorsCancellation(t *testing.T) {
	pool, lease, _, _ := inventoryLease(t)
	observations := make([]provider.Observation, 201)
	scanner := inventory.Scanner{Pool: pool, Reader: func(context.Context, provider.Binding) (provider.InventoryReader, error) {
		return scanReader{func(context.Context, string) (provider.InventoryPage, error) {
			return provider.InventoryPage{Data: observations}, nil
		}}, nil
	}}
	if err := scanner.Scan(context.Background(), lease); err == nil || err.Error() != "sync_limit_reached" {
		t.Fatal("oversized page", err)
	}
	calls := 0
	scanner.Reader = func(context.Context, provider.Binding) (provider.InventoryReader, error) {
		return scanReader{func(ctx context.Context, _ string) (provider.InventoryPage, error) {
			calls++
			<-ctx.Done()
			return provider.InventoryPage{}, ctx.Err()
		}}, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	started := time.Now()
	if scanner.Scan(ctx, lease) == nil || calls != 1 || time.Since(started) > time.Second {
		t.Fatal("deadline did not stop scan")
	}
}
