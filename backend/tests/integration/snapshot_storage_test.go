//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"sama/backend/internal/inventory"
	"sama/backend/internal/provider"
)

func TestSnapshotPersistenceAndTypeConstraints(t *testing.T) {
	pool := identityDatabase(t)
	_, scope := seedIdentity(t, pool)
	ctx := context.Background()
	connection, run := entityID(t), entityID(t)
	if _, err := pool.Exec(ctx, `INSERT INTO connections(id,workspace_id,provider,api_family,account_identity,label) VALUES($1,$2,'fixture','compute','account','Fixture')`, connection, scope); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO sync_runs(id,workspace_id,connection_id,resource_type,scope,generation) VALUES($1,$2,$3,'storage.snapshot','global',$1)`, run, scope, connection); err != nil {
		t.Fatal(err)
	}
	size := int64(1234)
	created := time.Now().UTC().Truncate(time.Second)
	metadata, err := inventory.SnapshotMetadata(provider.SnapshotObservation{NativeID: "image-1", SourceNativeID: "server-1", SourceType: "compute.server", Status: "available", CreatedAt: &created, SizeBytes: &size, ObservedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	o := inventory.Observation{ID: entityID(t), Workspace: scope, Connection: connection, ResourceType: inventory.SnapshotType, Family: "compute", Scope: "global", NativeID: "image-1", Status: "available", Name: "Snapshot", Generation: run, ObservedAt: time.Now(), Details: metadata}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	id, err := inventory.Upsert(ctx, tx, o)
	if err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	o.ID = entityID(t)
	o.Status = "unknown"
	o.Details.Snapshot.StatusReason = "not_reported"
	tx, err = pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	again, err := inventory.Upsert(ctx, tx, o)
	if err != nil || again != id {
		t.Fatal("snapshot identity changed", again, err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	var kind, status string
	var version int64
	var data []byte
	if err = pool.QueryRow(ctx, `SELECT resource_type,status,version,details FROM resources WHERE id=$1`, id).Scan(&kind, &status, &version, &data); err != nil {
		t.Fatal(err)
	}
	var details inventory.Details
	if json.Unmarshal(data, &details) != nil || details.Snapshot == nil || details.Snapshot.SourceNativeID != "server-1" || details.Snapshot.SizeBytes == nil || *details.Snapshot.SizeBytes != 1234 || !details.Snapshot.CreatedAt.Equal(created) || kind != inventory.SnapshotType || status != "unknown" || version != 2 {
		t.Fatal("metadata round trip", kind, status, version, string(data))
	}
	for _, bad := range []string{
		`{"snapshot":{"user_data":"synthetic-secret"}}`,
		`{"snapshot":{"size_bytes":-1}}`,
		`{"snapshot":{"size_bytes":1.5}}`,
		`{"snapshot":{"size_gb":"12"}}`,
		`{"snapshot":{"source_type":"compute.server"}}`,
		`{"snapshot":{"regions":["ok",12]}}`,
		`{"snapshot":{"created_at":"tomorrow"}}`,
		`{"region":"EU","snapshot":{}}`,
		`{"machine_type":"server"}`,
	} {
		if _, err = pool.Exec(ctx, `UPDATE resources SET details=$2 WHERE id=$1`, id, bad); err == nil {
			t.Fatal("unsafe details accepted", bad)
		}
	}
	if _, err = pool.Exec(ctx, `UPDATE resources SET status='running' WHERE id=$1`, id); err == nil {
		t.Fatal("server status accepted for snapshot")
	}
	if _, err = pool.Exec(ctx, `UPDATE resources SET resource_type='compute.server' WHERE id=$1`, id); err == nil {
		t.Fatal("snapshot metadata accepted as server")
	}
	if _, err = pool.Exec(ctx, `INSERT INTO inventory_staging SELECT * FROM resources WHERE id=$1`, id); err == nil {
		t.Fatal("staging accepted published version2")
	}
	if _, err = pool.Exec(ctx, `INSERT INTO inventory_staging(id,workspace_id,connection_id,api_family,resource_type,scope,native_id,native_status,status,name,details_version,details,observed_at,generation,version)
 SELECT $2,workspace_id,connection_id,api_family,resource_type,scope,native_id,native_status,status,name,details_version,details,observed_at,generation,1 FROM resources WHERE id=$1`, id, entityID(t)); err != nil {
		t.Fatal("typed staging rejected", err)
	}
	if _, err = pool.Exec(ctx, `UPDATE inventory_staging SET details='{"snapshot":{"password":"synthetic-secret"}}' WHERE workspace_id=$1`, scope); err == nil {
		t.Fatal("staging allowlist bypassed")
	}
}
