//go:build integration

package integration

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"sama/backend/internal/operation"
)

func TestOperationTransitionFencesLeaseAndRollsBackFailedEvent(t *testing.T) {
	pool, scopeLease, actor, target := operationFixture(t)
	review := storedReview(t, pool, scopeLease, actor, target)
	id := storedOperation(t, pool, scopeLease, actor, review, target, "state-key")
	lease := scopeLease
	lease.ID = entityID(t)
	lease.Owner = entityID(t)
	lease.Token = entityID(t)
	lease.Type = "operation_dispatch"
	lease.PayloadVersion = 3
	lease.Deadline = time.Now().Add(time.Hour)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `INSERT INTO jobs(id,workspace_id,connection_id,type,payload_version,payload,operation_id,deadline,state,lease_owner,lease_token,lease_expires_at)
 VALUES($1,$2,$3,'operation_dispatch',3,jsonb_build_object('connection_id',$3::uuid::text,'operation_id',$4::uuid::text),$4,$5,'running',$6,$7,clock_timestamp()+interval '60 seconds')`, lease.ID, lease.Workspace, lease.Connection, id, lease.Deadline, lease.Owner, lease.Token); err != nil {
		t.Fatal(err)
	}
	stale := lease
	stale.Token = entityID(t)
	if operation.Transition(ctx, pool, stale, id, "queued", "dispatching", uuid.Nil) != operation.ErrState {
		t.Fatal("stale lease advanced state")
	}
	if err := operation.Transition(ctx, pool, lease, id, "queued", "dispatching", uuid.Nil); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `CREATE FUNCTION reject_operation_event() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic event failure'; END $$;
 CREATE TRIGGER reject_operation_event BEFORE INSERT ON operation_events FOR EACH ROW EXECUTE FUNCTION reject_operation_event()`); err != nil {
		t.Fatal(err)
	}
	if operation.Transition(ctx, pool, lease, id, "dispatching", "reconciling", uuid.Nil) == nil {
		t.Fatal("failed event ignored")
	}
	var state string
	var count int
	if pool.QueryRow(ctx, `SELECT state,event_sequence FROM operations WHERE id=$1`, id).Scan(&state, &count) != nil || state != "dispatching" || count != 1 {
		t.Fatal("state/event atomicity lost")
	}
	if _, err := pool.Exec(ctx, `DROP TRIGGER reject_operation_event ON operation_events`); err != nil {
		t.Fatal(err)
	}
	if operation.Transition(ctx, pool, lease, id, "dispatching", "reconciling", uuid.Nil) != nil || operation.Transition(ctx, pool, lease, id, "reconciling", "unknown", uuid.Nil) != nil {
		t.Fatal("unknown transition")
	}
	if operation.Transition(ctx, pool, lease, id, "unknown", "reconciling", uuid.Nil) != operation.ErrState {
		t.Fatal("unaudited recovery")
	}
	if _, err := pool.Exec(ctx, `UPDATE jobs SET lease_expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`, lease.ID); err != nil {
		t.Fatal(err)
	}
	if operation.Transition(ctx, pool, lease, id, "unknown", "reconciling", entityID(t)) != operation.ErrState {
		t.Fatal("expired/unaudited worker changed unknown")
	}
}
