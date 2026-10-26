//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"sama/backend/internal/inventory"
	"sama/backend/internal/job"
	"sama/backend/internal/operation"
	"sama/backend/internal/provider"
)

func TestSnapshotHTTPMetadataRefreshAndClosedMutationAcceptance(t *testing.T) {
	pool, scope, actor, _ := operationFixture(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `UPDATE connections SET verified_capabilities=ARRAY['compute.server.snapshot_create','storage.snapshot.delete'],permissions_verified_at=clock_timestamp() WHERE id=$1`, scope.Connection); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE connection_grants SET actions=ARRAY['read','refresh','high_impact'] WHERE user_id=$1`, actor); err != nil {
		t.Fatal(err)
	}
	closed := operation.NewService(pool, nil, true, nil).WithPolicy(operation.NewPolicy([]string{"fixture/compute:storage.snapshot.delete"}, nil))
	post := operationHTTP(t, pool, actor, closed)
	get := inventoryHTTP(t, pool, actor, nil)
	base := "/api/v1/workspaces/" + scope.Workspace.String()
	syncPath := base + "/connections/" + scope.Connection.String() + "/syncs"
	response := post(syncPath, `{"resource_type":"storage.snapshot"}`, "", true)
	var accepted struct {
		ID string `json:"sync_id"`
	}
	if response.Code != 202 || json.Unmarshal(response.Body.Bytes(), &accepted) != nil {
		t.Fatal(response.Code, response.Body.String())
	}
	var status inventory.SyncStatus
	response = get(base + "/syncs/" + accepted.ID)
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &status) != nil || status.Inventory.LastSuccessAt != nil || status.Inventory.State != "never_observed" {
		t.Fatal("server freshness used for snapshots", response.Code, response.Body.String())
	}
	calls := 0
	scanner := inventory.Scanner{Pool: pool, Snapshots: func(context.Context, provider.Binding) (provider.SnapshotReader, error) {
		return snapshotScanReader{func(string) (provider.SnapshotPage, error) {
			calls++
			first := snapshotObservation("image-1", "unknown")
			first.SourceNativeID = "operation-target"
			second := snapshotObservation("image-2", "pending")
			second.SourceNativeID = "operation-target"
			return provider.SnapshotPage{Data: []provider.SnapshotObservation{first, second}}, nil
		}}, nil
	}}
	lease, err := (job.Scheduler{Pool: pool, Now: func() time.Time { return time.Now().Add(time.Second) }}).Claim(ctx, entityID(t))
	if err != nil {
		t.Fatal(err)
	}
	if err = scanner.Execute(ctx, lease); err != nil {
		t.Fatal(err)
	}
	path := base + "/resources?type=storage.snapshot&source_native_id=operation-target&source_type=compute.server&connection_id=" + scope.Connection.String()
	response = get(path)
	var page inventory.ResourcePage
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &page) != nil || len(page.Data) != 2 {
		t.Fatal(response.Code, response.Body.String())
	}
	detailPath := base + "/resources/" + page.Data[0].ID.String()
	response = get(detailPath)
	var detail inventory.ResourceDetail
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &detail) != nil || detail.Type != inventory.SnapshotType || detail.Details.Snapshot == nil || detail.Details.Snapshot.SizeBytes != nil || detail.Details.Snapshot.CreatedAt != nil {
		t.Fatal(response.Code, response.Body.String())
	}
	for _, capability := range detail.AvailableActions {
		if capability.Available || capability.Reason != "unsupported" {
			t.Fatal("closed adapter advertised", capability)
		}
	}
	if strings.Contains(response.Body.String(), "ciphertext") || strings.Contains(response.Body.String(), "synthetic-secret") {
		t.Fatal("unsafe response")
	}
	for _, bad := range []string{path + "&type=storage.snapshot", path + "&source_type=unknown", base + "/resources?type=storage.snapshot&source_type=invalid"} {
		if get(bad).Code != 400 {
			t.Fatal("invalid snapshot filter accepted", bad)
		}
	}
	if get("/api/v1/workspaces/"+entityID(t).String()+"/resources/"+detail.ID.String()).Code != 404 {
		t.Fatal("out-of-scope snapshot visible")
	}
	first, second := post(syncPath, `{"resource_type":"storage.snapshot"}`, "", true), post(syncPath, `{"resource_type":"storage.snapshot"}`, "", true)
	if first.Code != 202 || first.Body.String() != second.Body.String() {
		t.Fatal("snapshot HTTP refresh not coalesced")
	}
	for _, body := range []string{`{"resource_type":null}`, `{"resource_type":12}`, `{"resource_type":"other"}`, `{"resource_type":"storage.snapshot","scope":"chosen"}`} {
		if post(syncPath, body, "", true).Code != 400 {
			t.Fatal("invalid refresh body", body)
		}
	}
	if post(syncPath+"?type=storage.snapshot", `{}`, "", true).Code != 400 || post(syncPath, `{"resource_type":"storage.snapshot"}`, "", false).Code != 403 {
		t.Fatal("refresh mutation boundary")
	}
	body := `{"action":"storage.snapshot.delete","parameters":{}}`
	if response = post(detailPath+"/action-reviews", body, "", true); response.Code != 422 || !strings.Contains(response.Body.String(), "unsupported_capability") {
		t.Fatal("closed snapshot review accepted", response.Code, response.Body.String())
	}
	var reviews, operations int
	if pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM action_reviews),(SELECT count(*) FROM operations)`).Scan(&reviews, &operations) != nil || reviews != 0 || operations != 0 || calls != 1 {
		t.Fatal("HTTP caused provider effect or review persistence")
	}
	if _, err = pool.Exec(ctx, `DELETE FROM connection_grants WHERE user_id=$1`, actor); err != nil {
		t.Fatal(err)
	}
	if get(detailPath).Code != 404 {
		t.Fatal("revoked snapshot read visible")
	}
}
