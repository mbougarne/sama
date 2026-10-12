//go:build integration

package integration

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"sama/backend/internal/inventory"
	"sama/backend/internal/job"
	"sama/backend/internal/operation"
	"sama/backend/internal/provider"
)

func operationFixture(t *testing.T) (*pgxpool.Pool, job.Lease, uuid.UUID, uuid.UUID) {
	t.Helper()
	pool, lease, actor, _ := inventoryLease(t)
	scanner := inventory.Scanner{Pool: pool, Reader: func(context.Context, provider.Binding) (provider.InventoryReader, error) {
		return scanReader{func(context.Context, string) (provider.InventoryPage, error) {
			o := scanObservation("operation-target")
			o.Status = "stopped"
			return provider.InventoryPage{Data: []provider.Observation{o}}, nil
		}}, nil
	}}
	if err := scanner.Execute(context.Background(), lease); err != nil {
		t.Fatal(err)
	}
	var resource uuid.UUID
	if pool.QueryRow(context.Background(), `SELECT id FROM resources WHERE connection_id=$1`, lease.Connection).Scan(&resource) != nil {
		t.Fatal("operation resource")
	}
	return pool, lease, actor, resource
}

func storedReview(t *testing.T, pool *pgxpool.Pool, lease job.Lease, actor, target uuid.UUID) uuid.UUID {
	t.Helper()
	id := entityID(t)
	_, err := pool.Exec(context.Background(), `INSERT INTO action_reviews(id,workspace_id,actor_id,connection_id,resource_id,action,intent_version,intent,intent_hash,precondition_hash,credential_version,policy_version,high_impact,target_name,expires_at)
 SELECT $1,$2,$3,$4,$5,'compute.server.power_on',1,'{"action":"compute.server.power_on","parameters":{}}',$6,$6,1,w.policy_version,false,'Safe',clock_timestamp()+interval '5 minutes' FROM workspaces w WHERE w.id=$2`, id, lease.Workspace, actor, lease.Connection, target, make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func storedOperation(t *testing.T, pool *pgxpool.Pool, lease job.Lease, actor, review, target uuid.UUID, key string) uuid.UUID {
	t.Helper()
	id := entityID(t)
	_, err := pool.Exec(context.Background(), `INSERT INTO operations(id,workspace_id,actor_id,connection_id,resource_id,action,intent_version,intent,intent_hash,idempotency_key,review_id,credential_version,policy_version,state,deadline)
 SELECT $1,$2,$3,$4,$5,r.action,r.intent_version,r.intent,r.intent_hash,$6,r.id,r.credential_version,r.policy_version,'queued',clock_timestamp()+interval '30 minutes' FROM action_reviews r WHERE r.id=$7`, id, lease.Workspace, actor, lease.Connection, target, key, review)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestOperationStorageTenantReferencesImmutableIntentAndEventSequence(t *testing.T) {
	pool, lease, actor, target := operationFixture(t)
	review := storedReview(t, pool, lease, actor, target)
	id := storedOperation(t, pool, lease, actor, review, target, "storage-key")
	ctx := context.Background()
	for _, sql := range []string{
		`UPDATE operations SET action='compute.server.reboot',intent='{"action":"compute.server.reboot","parameters":{}}' WHERE id=$1`,
		`UPDATE operations SET intent_hash=decode(repeat('01',32),'hex') WHERE id=$1`,
		`UPDATE operations SET idempotency_key='another' WHERE id=$1`,
		`UPDATE operations SET credential_version=2 WHERE id=$1`,
	} {
		if _, err := pool.Exec(ctx, sql, id); err == nil {
			t.Fatal("accepted intent changed")
		}
	}
	_, other := seedIdentity(t, pool)
	if _, err := pool.Exec(ctx, `UPDATE action_reviews SET workspace_id=$2 WHERE id=$1`, review, other); err == nil {
		t.Fatal("cross-tenant target allowed")
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if operation.AppendEvent(ctx, tx, lease.Workspace, id, 1, "", "queued") != nil || tx.Commit(ctx) != nil {
		t.Fatal("event append")
	}
	for _, sql := range []string{
		`INSERT INTO operation_events(id,workspace_id,operation_id,sequence,from_state,to_state) SELECT $1,$2,$3,1,'queued','dispatching' WHERE $4::uuid IS NOT NULL`,
		`INSERT INTO operation_events(id,workspace_id,operation_id,sequence,from_state,to_state) SELECT $1,$4,$3,2,'queued','dispatching' WHERE $2::uuid IS NOT NULL`,
	} {
		if _, err := pool.Exec(ctx, sql, entityID(t), lease.Workspace, id, other); err == nil {
			t.Fatal("event uniqueness/tenant fence failed")
		}
	}
	if _, err := pool.Exec(ctx, `UPDATE action_reviews SET intent=intent||'{"credential":"synthetic-secret"}' WHERE id=$1`, review); err == nil {
		t.Fatal("untyped secret intent accepted")
	}
}
