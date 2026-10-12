//go:build integration

package integration

import (
	"context"
	"sama/backend/internal/inventory"
	"sama/backend/internal/job"
	"sama/backend/internal/platform/outbound"
	"sama/backend/internal/provider"
	"sama/backend/internal/workspace"
	"testing"
	"time"
)

// HTTP schemas are tested in each adapter package. These synthetic normalized
// ports test both provider families through the real PostgreSQL publish/query
// boundary; no production account identity or credential qualification is claimed.
func TestTwoProviderScopesRemainIsolatedWhenOneRefreshFails(t *testing.T) {
	pool, first, actor, _ := inventoryLeaseForFamily(t, "hetzner.cloud")
	ctx := context.Background()
	second := entityID(t)
	for _, stmt := range []string{
		`INSERT INTO connections(id,workspace_id,provider,api_family,account_identity,label) VALUES($1,$2,'digitalocean','digitalocean.platform','synthetic-account','Second')`,
		`INSERT INTO credential_versions(workspace_id,connection_id,version,ciphertext,nonce,wrapped_data_key,wrapping_nonce,encryption_format,master_key_id) VALUES($2,$1,1,'c','n','k','w',1,'fixture')`,
		`UPDATE connections SET active_credential_version=1,status='active' WHERE id=$1 AND workspace_id=$2`,
	} {
		if _, err := pool.Exec(ctx, stmt, second, first.Workspace); err != nil {
			t.Fatal(err)
		}
	}
	if err := workspace.SetGrant(ctx, pool, actor, first.Workspace, second, actor, []string{"read", "refresh"}, "second-grant"); err != nil {
		t.Fatal(err)
	}
	failing := false
	scanner := inventory.Scanner{Pool: pool, Reader: func(_ context.Context, b provider.Binding) (provider.InventoryReader, error) {
		expected := "hetzner.cloud"
		if b.Connection() == second {
			expected = "digitalocean.platform"
		}
		if b.Family() != expected || b.Workspace() != first.Workspace || b.Version() != 1 {
			t.Fatal("binding crossed providers")
		}
		return scanReader{func(_ context.Context, cursor string) (provider.InventoryPage, error) {
			if failing && b.Connection() == first.Connection && cursor == "last" {
				return provider.InventoryPage{}, &outbound.Failure{Code: "provider_permission_denied"}
			}
			o := scanObservation("42")
			o.Details.Region = "nbg1"
			if b.Connection() == second {
				o.Details.Region = "ams3"
			}
			page := provider.InventoryPage{Data: []provider.Observation{o}}
			if failing && b.Connection() == first.Connection {
				page.Next = "last"
			}
			return page, nil
		}}, nil
	}}
	if err := scanner.Execute(ctx, first); err != nil {
		t.Fatal(err)
	}
	refresh := func(connectionID string) job.Lease {
		t.Helper()
		lease, err := (job.Scheduler{Pool: pool, Now: func() time.Time { return time.Now().Add(time.Second) }}).Claim(ctx, entityID(t))
		if err != nil || lease.Connection.String() != connectionID {
			t.Fatal("claim", err)
		}
		return lease
	}
	if _, err := inventory.Refresh(ctx, pool, actor, first.Workspace, second); err != nil {
		t.Fatal(err)
	}
	if err := scanner.Execute(ctx, refresh(second.String())); err != nil {
		t.Fatal(err)
	}
	failing = true
	if _, err := inventory.Refresh(ctx, pool, actor, first.Workspace, first.Connection); err != nil {
		t.Fatal(err)
	}
	if err := scanner.Execute(ctx, refresh(first.Connection.String())); err == nil {
		t.Fatal("denial ignored")
	}
	page, err := inventory.List(ctx, pool, actor, first.Workspace, inventory.Filters{}, "", 50)
	if err != nil || len(page.Data) != 2 || page.Data[0].ID == page.Data[1].ID {
		t.Fatal("provider identity collision", page, err)
	}
	var active int
	if pool.QueryRow(ctx, `SELECT count(*) FROM resources WHERE native_id='42' AND tombstoned_at IS NULL`).Scan(&active) != nil || active != 2 {
		t.Fatal("failed provider erased inventory")
	}
}
