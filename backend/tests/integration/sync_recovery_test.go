//go:build integration

package integration

import (
	"context"
	"testing"
	"time"

	"sama/backend/internal/inventory"
	"sama/backend/internal/job"
	"sama/backend/internal/provider"
)

func TestInterruptedScanRestartsUnpublishedGenerationAndFencesStaleOwner(t *testing.T) {
	pool, lease, _, run := inventoryLease(t)
	ctx := context.Background()
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	old := inventory.Scanner{Pool: pool, Reader: func(context.Context, provider.Binding) (provider.InventoryReader, error) {
		return scanReader{func(ctx context.Context, cursor string) (provider.InventoryPage, error) {
			if cursor == "" {
				return provider.InventoryPage{Data: []provider.Observation{scanObservation("partial-old")}, Next: "blocked"}, nil
			}
			entered <- struct{}{}
			select {
			case <-release:
				return provider.InventoryPage{Data: []provider.Observation{scanObservation("late-old")}}, nil
			case <-ctx.Done():
				return provider.InventoryPage{}, ctx.Err()
			}
		}}, nil
	}}
	done := make(chan error, 1)
	go func() { done <- old.Scan(ctx, lease) }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("partial page never staged")
	}
	var previous string
	if pool.QueryRow(ctx, `SELECT generation::text FROM sync_runs WHERE id=$1`, run).Scan(&previous) != nil {
		t.Fatal("generation")
	}
	if _, err := pool.Exec(ctx, `UPDATE jobs SET lease_expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`, lease.ID); err != nil {
		t.Fatal(err)
	}
	successor, err := (job.Scheduler{Pool: pool, Now: func() time.Time { return time.Now().Add(time.Second) }}).Claim(ctx, entityID(t))
	if err != nil {
		t.Fatal(err)
	}
	fresh := inventory.Scanner{Pool: pool, Reader: func(context.Context, provider.Binding) (provider.InventoryReader, error) {
		return scanReader{func(context.Context, string) (provider.InventoryPage, error) {
			return provider.InventoryPage{Data: []provider.Observation{scanObservation("fresh")}}, nil
		}}, nil
	}}
	if err := fresh.Execute(ctx, successor); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-done; err == nil {
		t.Fatal("stale owner wrote late page")
	}
	var current string
	var count int
	if pool.QueryRow(ctx, `SELECT generation::text FROM sync_runs WHERE id=$1`, run).Scan(&current) != nil || current == previous {
		t.Fatal("restart reused old generation")
	}
	if pool.QueryRow(ctx, `SELECT count(*) FROM resources WHERE native_id='fresh' AND tombstoned_at IS NULL`).Scan(&count) != nil || count != 1 {
		t.Fatal("fresh inventory missing")
	}
	if pool.QueryRow(ctx, `SELECT count(*) FROM resources WHERE native_id<>'fresh'`).Scan(&count) != nil || count != 0 {
		t.Fatal("partial/late pages leaked")
	}
	if pool.QueryRow(ctx, `SELECT count(*) FROM inventory_staging WHERE generation::text=$1`, previous).Scan(&count) != nil || count != 0 {
		t.Fatal("restart retained unsafe partial staging")
	}
}
