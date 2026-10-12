//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"testing"

	"sama/backend/internal/inventory"
	"sama/backend/internal/provider"
)

func TestResourceListHTTPStrictQueriesAndSafeLocalResponses(t *testing.T) {
	pool, lease, actor, _ := inventoryLease(t)
	calls := 0
	scanner := inventory.Scanner{Pool: pool, Reader: func(context.Context, provider.Binding) (provider.InventoryReader, error) {
		return scanReader{func(context.Context, string) (provider.InventoryPage, error) {
			calls++
			return provider.InventoryPage{Data: []provider.Observation{scanObservation("one"), scanObservation("two")}}, nil
		}}, nil
	}}
	if err := scanner.Execute(context.Background(), lease); err != nil {
		t.Fatal(err)
	}
	request := inventoryHTTP(t, pool, actor)
	path := "/api/v1/workspaces/" + lease.Workspace.String() + "/resources"
	first := request(path + "?limit=1")
	var page inventory.ResourcePage
	if first.Code != 200 || json.Unmarshal(first.Body.Bytes(), &page) != nil || len(page.Data) != 1 || page.NextCursor == nil || page.Data[0].NativeStatus != "unrecognized" || page.Data[0].Inventory.LastSuccessAt == nil {
		t.Fatal("resource response", first.Code, first.Body.String())
	}
	second := request(path + "?limit=1&cursor=" + *page.NextCursor)
	var next inventory.ResourcePage
	if second.Code != 200 || json.Unmarshal(second.Body.Bytes(), &next) != nil || len(next.Data) != 1 || next.Data[0].ID == page.Data[0].ID || next.NextCursor != nil {
		t.Fatal("HTTP cursor")
	}
	for _, query := range []string{"limit=0", "limit=201", "limit=1&limit=2", "cursor=bad", "connection_id=bad", "region=%zz", "region=one&region=two", "unknown=secret", "type=other", "account="} {
		if w := request(path + "?" + query); w.Code != 400 {
			t.Fatal(query, w.Code)
		}
	}
	if w := request(path + "?connection_id=" + lease.Connection.String() + "&region=region&account=account"); w.Code != 200 {
		t.Fatal("filters", w.Code)
	}
	if calls != 1 {
		t.Fatal("inventory GET called provider", calls)
	}
	if _, err := pool.Exec(context.Background(), `DELETE FROM connection_grants WHERE user_id=$1`, actor); err != nil {
		t.Fatal(err)
	}
	denied := request(path + "?connection_id=" + lease.Connection.String())
	var empty inventory.ResourcePage
	if denied.Code != 200 || json.Unmarshal(denied.Body.Bytes(), &empty) != nil || len(empty.Data) != 0 {
		t.Fatal("denied connection leaked")
	}
}
