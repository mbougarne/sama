//go:build integration

package integration

import (
	"context"
	"sama/backend/internal/job"
	"testing"
	"time"
)

func TestDurableJobPayloads(t *testing.T) {
	pool := identityDatabase(t)
	_, scope := seedIdentity(t, pool)
	_, foreign := seedIdentity(t, pool)
	ctx := context.Background()
	id := entityID(t)
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO connections(id,workspace_id,provider,api_family,account_identity,label) VALUES($1,$2,'fixture','compute','account','Fixture')`, id, scope)
	exec(`INSERT INTO credential_versions(workspace_id,connection_id,version,ciphertext,nonce,wrapped_data_key,wrapping_nonce,encryption_format,master_key_id) VALUES($1,$2,1,'sentinel','n','k','w',1,'fixture')`, scope, id)
	exec(`UPDATE connections SET status='active',active_credential_version=1 WHERE id=$1`, id)
	record := job.Record{ID: entityID(t), Workspace: scope, Connection: id, Type: "connection_refresh", PayloadVersion: 1, Deadline: time.Now().Add(time.Hour)}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := job.Insert(ctx, tx, record); err != nil {
		t.Fatal(err)
	}
	if tx.Commit(ctx) != nil {
		t.Fatal("store commit")
	}
	for _, sql := range []string{`UPDATE jobs SET payload_version=2`, `UPDATE jobs SET payload='{"token":"secret-sentinel"}'`, `UPDATE jobs SET payload=jsonb_build_object('connection_id',connection_id::text,'raw',repeat('x',2000))`, `UPDATE jobs SET state='running'`, `UPDATE jobs SET type='unknown'`} {
		if _, err := pool.Exec(ctx, sql); err == nil {
			t.Fatal("invalid job accepted", sql)
		}
	}
	if _, err := pool.Exec(ctx, `UPDATE jobs SET workspace_id=$1`, foreign); err == nil {
		t.Fatal("cross-workspace job reference")
	}
}
