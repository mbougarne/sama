//go:build integration

package integration

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"sama/backend/internal/inventory"
	"sama/backend/internal/workspace"
)

func TestResourceQueryFiltersAndCursorCannotExposeDeniedConnections(t *testing.T) {
	pool, lease, actor, run := inventoryLease(t)
	ctx := context.Background()
	// Equal observation times require the UUID tie-breaker; use enough rows to
	// discriminate the actual default from an accidental unbounded query.
	for i := 0; i < 53; i++ {
		id := entityID(t)
		if _, err := pool.Exec(ctx, `INSERT INTO resources(id,workspace_id,connection_id,api_family,resource_type,scope,native_id,native_status,status,name,details_version,details,observed_at,generation)
   VALUES($1,$2,$3,'compute','compute.server','global',$4,'native','unknown','Safe',1,'{"region":"test-region"}','2026-01-01T00:00:00Z',$5)`, id, lease.Workspace, lease.Connection, fmt.Sprintf("server-%d", i), run); err != nil {
			t.Fatal(err)
		}
	}
	// A completed sibling scope sharing a generation value cannot make an
	// unpublished scope visible or duplicate its rows.
	if _, err := pool.Exec(ctx, `INSERT INTO sync_runs(id,workspace_id,connection_id,resource_type,scope,generation,status) VALUES($1,$2,$3,'compute.server','other',$4,'succeeded')`, entityID(t), lease.Workspace, lease.Connection, run); err != nil {
		t.Fatal(err)
	}
	unpublished, err := inventory.List(ctx, pool, actor, lease.Workspace, inventory.Filters{}, "", 50)
	if err != nil || len(unpublished.Data) != 0 {
		t.Fatal("unpublished scope leaked", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE sync_runs SET status='succeeded',scan_complete=true,completed_at=clock_timestamp() WHERE id=$1`, run); err != nil {
		t.Fatal(err)
	}
	first, err := inventory.List(ctx, pool, actor, lease.Workspace, inventory.Filters{}, "", 0)
	if err != nil || len(first.Data) != 50 || first.NextCursor == nil {
		t.Fatal("default pagination", err, len(first.Data))
	}
	second, err := inventory.List(ctx, pool, actor, lease.Workspace, inventory.Filters{}, *first.NextCursor, 0)
	if err != nil || len(second.Data) != 3 || second.NextCursor != nil || first.Data[49].ID.String() >= second.Data[0].ID.String() {
		t.Fatal("cursor ordering", err)
	}
	for _, filters := range []inventory.Filters{{Provider: "other"}, {Region: "other"}, {Account: "other"}, {Connection: entityID(t)}} {
		if _, err = inventory.List(ctx, pool, actor, lease.Workspace, filters, *first.NextCursor, 50); err != workspace.ErrInvalid {
			t.Fatal("changed filters accepted cursor", err)
		}
	}
	if _, err = inventory.List(ctx, pool, actor, lease.Workspace, inventory.Filters{}, "malformed", 50); err != workspace.ErrInvalid {
		t.Fatal("malformed cursor", err)
	}
	if _, err = inventory.List(ctx, pool, actor, lease.Workspace, inventory.Filters{}, "", 201); err != workspace.ErrInvalid {
		t.Fatal("unbounded page", err)
	}
	_, other := seedIdentity(t, pool)
	if _, err = inventory.List(ctx, pool, actor, other, inventory.Filters{}, *first.NextCursor, 50); err != workspace.ErrInvalid {
		t.Fatal("cross-tenant cursor", err)
	}
	filtered, err := inventory.List(ctx, pool, actor, lease.Workspace, inventory.Filters{Provider: "missing"}, "", 50)
	if err != nil || len(filtered.Data) != 0 {
		t.Fatal("provider filter", err)
	}
	exact, err := inventory.List(ctx, pool, actor, lease.Workspace, inventory.Filters{Connection: lease.Connection, Region: "test-region", Account: "account"}, "", 200)
	if err != nil || len(exact.Data) != 53 {
		t.Fatal("filters", err)
	}
	if _, err = pool.Exec(ctx, `DELETE FROM connection_grants WHERE user_id=$1`, actor); err != nil {
		t.Fatal(err)
	}
	for _, filters := range []inventory.Filters{{}, {Connection: lease.Connection}, {Account: "account"}, {Region: "test-region"}} {
		page, err := inventory.List(ctx, pool, actor, lease.Workspace, filters, "", 50)
		if err != nil || len(page.Data) != 0 {
			t.Fatal("denied resource exposed", err)
		}
	}
	after, err := inventory.List(ctx, pool, actor, lease.Workspace, inventory.Filters{}, *first.NextCursor, 50)
	if err != nil || len(after.Data) != 0 {
		t.Fatal("cursor preserved revoked authority", err)
	}
	if _, err = inventory.List(ctx, pool, actor, lease.Workspace, inventory.Filters{Connection: uuid.New()}, "", 50); err != workspace.ErrInvalid {
		t.Fatal("non-v7 filter")
	}
}
