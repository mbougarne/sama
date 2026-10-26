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
	"sama/backend/internal/workspace"
)

type snapshotScanReader struct {
	page func(string) (provider.SnapshotPage, error)
}

func (r snapshotScanReader) SnapshotPage(_ context.Context, _ provider.Binding, cursor string) (provider.SnapshotPage, error) {
	return r.page(cursor)
}
func (r snapshotScanReader) ReadSnapshot(context.Context, provider.Binding, string) (provider.SnapshotObservation, error) {
	return provider.SnapshotObservation{}, errors.New("unused")
}
func snapshotObservation(id, status string) provider.SnapshotObservation {
	return provider.SnapshotObservation{NativeID: id, Name: "Snapshot", SourceType: "compute.server", SourceNativeID: "server", Status: status, StatusReason: "not_reported", Regions: []string{}, ObservedAt: time.Now()}
}
func TestSnapshotScanPublicationAndScopedQueries(t *testing.T) {
	pool, serverLease, actor, _ := inventoryLease(t)
	ctx := context.Background()
	scanner := inventory.Scanner{Pool: pool, Reader: func(context.Context, provider.Binding) (provider.InventoryReader, error) {
		return scanReader{func(context.Context, string) (provider.InventoryPage, error) {
			return provider.InventoryPage{Data: []provider.Observation{scanObservation("server")}}, nil
		}}, nil
	}}
	if err := scanner.Execute(ctx, serverLease); err != nil {
		t.Fatal(err)
	}
	claim := func(kind string) job.Lease {
		t.Helper()
		run, err := inventory.RefreshType(ctx, pool, actor, serverLease.Workspace, serverLease.Connection, kind)
		if err != nil {
			t.Fatal(err)
		}
		same, err := inventory.RefreshType(ctx, pool, actor, serverLease.Workspace, serverLease.Connection, kind)
		if err != nil || same != run {
			t.Fatal("refresh not coalesced", err)
		}
		lease, err := (job.Scheduler{Pool: pool, Now: func() time.Time { return time.Now().Add(time.Second) }}).Claim(ctx, entityID(t))
		if err != nil {
			t.Fatal(err)
		}
		return lease
	}
	scanner.Snapshots = func(_ context.Context, b provider.Binding) (provider.SnapshotReader, error) {
		if b.Workspace() != serverLease.Workspace || b.Connection() != serverLease.Connection || b.Version() != 1 {
			t.Fatal("snapshot binding")
		}
		return snapshotScanReader{func(string) (provider.SnapshotPage, error) {
			return provider.SnapshotPage{Data: []provider.SnapshotObservation{snapshotObservation("first", "unknown"), snapshotObservation("second", "pending")}}, nil
		}}, nil
	}
	snapLease := claim(inventory.SnapshotType)
	if err := scanner.Scan(ctx, snapLease); err != nil {
		t.Fatal(err)
	}
	filters := inventory.Filters{Type: inventory.SnapshotType, Connection: serverLease.Connection, SourceID: "server", SourceType: "compute.server"}
	page, err := inventory.List(ctx, pool, actor, serverLease.Workspace, filters, "", 50)
	if err != nil || len(page.Data) != 0 {
		t.Fatal("staged snapshots visible", page, err)
	}
	// A later server generation cannot cancel/tombstone an older snapshot scan.
	newerServer := claim("compute.server")
	if err = scanner.Execute(ctx, newerServer); err != nil {
		t.Fatal(err)
	}
	if err = scanner.Complete(ctx, snapLease, nil); err != nil {
		t.Fatal(err)
	}
	page, err = inventory.List(ctx, pool, actor, serverLease.Workspace, filters, "", 1)
	if err != nil || len(page.Data) != 1 || page.NextCursor == nil || page.Data[0].Type != inventory.SnapshotType {
		t.Fatal(page, err)
	}
	first := page.Data[0]
	next, err := inventory.List(ctx, pool, actor, serverLease.Workspace, filters, *page.NextCursor, 1)
	if err != nil || len(next.Data) != 1 || next.NextCursor != nil {
		t.Fatal(next, err)
	}
	if _, err = inventory.List(ctx, pool, actor, serverLease.Workspace, inventory.Filters{}, *page.NextCursor, 1); err != workspace.ErrInvalid {
		t.Fatal("cursor crossed type", err)
	}
	detail, err := inventory.Detail(ctx, pool, actor, serverLease.Workspace, first.ID, nil)
	if err != nil || detail.Details.Snapshot == nil || detail.Details.Snapshot.SourceNativeID != "server" || detail.Details.Snapshot.SizeBytes != nil {
		t.Fatal(detail, err)
	}
	if _, err = inventory.Detail(ctx, pool, actor, entityID(t), first.ID, nil); err != workspace.ErrNotFound {
		t.Fatal("cross-workspace detail", err)
	}
	serverPage, err := inventory.List(ctx, pool, actor, serverLease.Workspace, inventory.Filters{}, "", 50)
	if err != nil || len(serverPage.Data) != 1 || serverPage.Data[0].Type != "compute.server" {
		t.Fatal("server default changed", serverPage, err)
	}
	calls := 0
	scanner.Snapshots = func(context.Context, provider.Binding) (provider.SnapshotReader, error) {
		return snapshotScanReader{func(cursor string) (provider.SnapshotPage, error) {
			calls++
			if cursor != "" {
				return provider.SnapshotPage{}, errors.New("synthetic later page failure")
			}
			return provider.SnapshotPage{Data: []provider.SnapshotObservation{snapshotObservation("partial", "available")}, Next: "last"}, nil
		}}, nil
	}
	if err = scanner.Execute(ctx, claim(inventory.SnapshotType)); err == nil || calls != 2 {
		t.Fatal("partial scan published", err)
	}
	retained, err := inventory.List(ctx, pool, actor, serverLease.Workspace, filters, "", 50)
	if err != nil || len(retained.Data) != 2 || retained.Data[0].Inventory.FailureCode == nil {
		t.Fatal("last complete snapshots lost", retained, err)
	}
	serverPage, err = inventory.List(ctx, pool, actor, serverLease.Workspace, inventory.Filters{}, "", 50)
	if err != nil || len(serverPage.Data) != 1 || serverPage.Data[0].Inventory.FailureCode != nil {
		t.Fatal("snapshot failure polluted server freshness", serverPage, err)
	}
	scanner.Snapshots = func(context.Context, provider.Binding) (provider.SnapshotReader, error) {
		return snapshotScanReader{func(string) (provider.SnapshotPage, error) {
			return provider.SnapshotPage{Data: []provider.SnapshotObservation{snapshotObservation(first.NativeID, "unknown")}}, nil
		}}, nil
	}
	if err = scanner.Execute(ctx, claim(inventory.SnapshotType)); err != nil {
		t.Fatal(err)
	}
	retained, err = inventory.List(ctx, pool, actor, serverLease.Workspace, filters, "", 50)
	if err != nil || len(retained.Data) != 1 || retained.Data[0].ID != first.ID {
		t.Fatal("snapshot identity/tombstone", retained, err)
	}
	var servers int
	if pool.QueryRow(ctx, `SELECT count(*) FROM resources WHERE resource_type='compute.server' AND tombstoned_at IS NULL`).Scan(&servers) != nil || servers != 1 {
		t.Fatal("snapshot scan tombstoned server")
	}
	scanner.Snapshots = func(context.Context, provider.Binding) (provider.SnapshotReader, error) {
		return snapshotScanReader{func(string) (provider.SnapshotPage, error) { return provider.SnapshotPage{Next: "repeat"}, nil }}, nil
	}
	if err = scanner.Execute(ctx, claim(inventory.SnapshotType)); err == nil || err.Error() != "sync_limit_reached" {
		t.Fatal("snapshot cursor loop", err)
	}
	revoked := claim(inventory.SnapshotType)
	calls = 0
	scanner.Snapshots = func(context.Context, provider.Binding) (provider.SnapshotReader, error) {
		calls++
		return nil, errors.New("must not call")
	}
	if _, err = pool.Exec(ctx, `DELETE FROM connection_grants WHERE workspace_id=$1 AND user_id=$2`, serverLease.Workspace, actor); err != nil {
		t.Fatal(err)
	}
	if err = scanner.Execute(ctx, revoked); err == nil || calls != 0 {
		t.Fatal("revocation called provider", err)
	}
	retained, err = inventory.List(ctx, pool, actor, serverLease.Workspace, filters, "", 50)
	if err != nil || len(retained.Data) != 0 {
		t.Fatal("revoked read leaked snapshots", retained, err)
	}
}
