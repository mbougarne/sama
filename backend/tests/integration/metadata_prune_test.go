//go:build integration

package integration

import (
	"context"
	"testing"

	"sama/backend/internal/inventory"
	"sama/backend/internal/job"
	"sama/backend/internal/operation"
)

func TestMetadataPruningIsBoundedAndRetainsUnknownEvidence(t *testing.T) {
	pool, lease, actor, target := operationFixture(t)
	ctx := context.Background()
	base := storedReview(t, pool, lease, actor, target)
	expired := entityID(t)
	clone := func(id any) {
		t.Helper()
		if _, err := pool.Exec(ctx, `INSERT INTO action_reviews(id,workspace_id,actor_id,connection_id,resource_id,action,intent_version,intent,intent_hash,precondition_hash,credential_version,policy_version,high_impact,target_name,created_at,expires_at)
   SELECT $1,workspace_id,actor_id,connection_id,resource_id,action,intent_version,intent,intent_hash,precondition_hash,credential_version,policy_version,high_impact,target_name,clock_timestamp()-interval '9 days',clock_timestamp()-interval '9 days'+interval '5 minutes' FROM action_reviews WHERE id=$2`, id, base); err != nil {
			t.Fatal(err)
		}
	}
	clone(expired)
	consumed := entityID(t)
	clone(consumed)
	unknown := storedOperation(t, pool, lease, actor, consumed, target, "retain-unknown")
	if _, err := pool.Exec(ctx, `UPDATE action_reviews SET consumed_by=$2 WHERE id=$1`, consumed, unknown); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE operations SET state='unknown' WHERE id=$1`, unknown); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO jobs(id,workspace_id,connection_id,type,payload_version,payload,operation_id,state,created_at,completed_at,deadline)
 VALUES($1,$2,$3,'operation_dispatch',3,jsonb_build_object('connection_id',$3::uuid::text,'operation_id',$4::uuid::text),$4,'failed',clock_timestamp()-interval '9 days',clock_timestamp()-interval '8 days',clock_timestamp()+interval '1 day')`, entityID(t), lease.Workspace, lease.Connection, unknown); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 101; i++ {
		if _, err := pool.Exec(ctx, `INSERT INTO jobs(id,workspace_id,connection_id,type,payload_version,payload,state,created_at,completed_at,deadline)
   VALUES($1,$2,$3,'connection_refresh',1,jsonb_build_object('connection_id',$3::uuid::text),'failed',clock_timestamp()-interval '9 days',clock_timestamp()-interval '8 days',clock_timestamp()+interval '1 day')`, entityID(t), lease.Workspace, lease.Connection); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, `UPDATE sync_runs SET completed_at=clock_timestamp()-interval '8 days' WHERE connection_id=$1`, lease.Connection); err != nil {
		t.Fatal(err)
	}
	reviews, err := operation.PruneReviews(ctx, pool)
	if err != nil || reviews != 1 {
		t.Fatal("review retention", reviews, err)
	}
	staging, err := inventory.PruneStaging(ctx, pool)
	if err != nil || staging != 1 {
		t.Fatal("staging retention", staging, err)
	}
	jobs, err := job.PruneTerminal(ctx, pool)
	if err != nil || jobs != 100 {
		t.Fatal("job batch bound", jobs, err)
	}
	var count int
	for _, query := range []string{`SELECT count(*) FROM operations WHERE state='unknown'`, `SELECT count(*) FROM jobs WHERE operation_id IS NOT NULL`, `SELECT count(*) FROM action_reviews WHERE consumed_by IS NOT NULL`, `SELECT count(*) FROM resources`} {
		if pool.QueryRow(ctx, query).Scan(&count) != nil || count != 1 {
			t.Fatal("cleanup erased durable evidence")
		}
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := job.PruneTerminal(cancelled, pool); err == nil {
		t.Fatal("cancelled cleanup succeeded")
	}
	jobs, err = job.PruneTerminal(ctx, pool)
	if err != nil || jobs != 1 {
		t.Fatal("bounded follow-up batch", jobs, err)
	}
}
