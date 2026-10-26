//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"sama/backend/internal/inventory"
	"sama/backend/internal/provider"
)

func TestResourceDetailTypedFieldsAndCurrentAdvisoryActions(t *testing.T) {
	pool, lease, actor, _ := inventoryLease(t)
	ctx := context.Background()
	scanner := inventory.Scanner{Pool: pool, Reader: func(context.Context, provider.Binding) (provider.InventoryReader, error) {
		return scanReader{func(context.Context, string) (provider.InventoryPage, error) {
			o := scanObservation("server")
			o.Status = "stopped"
			o.NativeStatus = "off"
			return provider.InventoryPage{Data: []provider.Observation{o}}, nil
		}}, nil
	}}
	if err := scanner.Execute(ctx, lease); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE connections SET permissions_verified_at=clock_timestamp(),verified_capabilities=ARRAY['compute.server.read','compute.server.power_on'] WHERE id=$1`, lease.Connection); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE connection_grants SET actions=ARRAY['read','operate'] WHERE user_id=$1`, actor); err != nil {
		t.Fatal(err)
	}
	var id string
	if pool.QueryRow(ctx, `SELECT id::text FROM resources WHERE native_id='server'`).Scan(&id) != nil {
		t.Fatal("resource missing")
	}
	request := inventoryHTTP(t, pool, actor, map[string][]string{"fixture/compute": {"compute.server.read", "compute.server.power_on"}})
	path := "/api/v1/workspaces/" + lease.Workspace.String() + "/resources/" + id
	check := func(expected string) {
		t.Helper()
		w := request(path)
		var result inventory.ResourceDetail
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil || result.DetailsVersion != 1 || result.Details.MachineType != "small" || result.NativeStatus != "off" || result.Inventory.LastSuccessAt == nil {
			t.Fatal("detail shape", w.Code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), "credential") || strings.Contains(w.Body.String(), "ciphertext") || strings.Contains(w.Body.String(), "payload") {
			t.Fatal("internal fields leaked")
		}
		if len(result.AvailableActions) != 6 || result.AvailableActions[1].Reason != expected {
			t.Fatal("action reason", result.AvailableActions)
		}
	}
	check("available")
	if _, err := pool.Exec(ctx, `UPDATE connection_grants SET actions=ARRAY['read'] WHERE user_id=$1`, actor); err != nil {
		t.Fatal(err)
	}
	check("denied")
	if request(path+"?anything=secret").Code != 400 {
		t.Fatal("detail query accepted")
	}
	_, other := seedIdentity(t, pool)
	if request("/api/v1/workspaces/"+other.String()+"/resources/"+id).Code != 404 {
		t.Fatal("cross-workspace detail leaked")
	}
	if _, err := pool.Exec(ctx, `DELETE FROM connection_grants WHERE user_id=$1`, actor); err != nil {
		t.Fatal(err)
	}
	if request(path).Code != 404 || request("/api/v1/workspaces/"+lease.Workspace.String()+"/resources/"+entityID(t).String()).Code != 404 {
		t.Fatal("denied differs from missing")
	}
}
