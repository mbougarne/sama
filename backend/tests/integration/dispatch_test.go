//go:build integration

package integration

import (
	"context"
	"crypto/sha256"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"sama/backend/internal/identity"
	"sama/backend/internal/job"
	"sama/backend/internal/operation"
	"sama/backend/internal/provider"
)

func dispatchSetup(t *testing.T) (operation.Dispatcher, *pgxpool.Pool, job.Lease, uuid.UUID, uuid.UUID, *mutationFixture, *provider.Preconditions) {
	t.Helper()
	s, pool, scope, actor, target, pre, _ := acceptanceSetup(t)
	confirmation := prepareAcceptance(t, s, actor, scope.Workspace, target)
	accepted, err := s.Confirm(context.Background(), identity.Principal{UserID: actor}, scope.Workspace, "dispatch-key", confirmation, "dispatch-test")
	if err != nil {
		t.Fatal(err)
	}
	f := newMutationFixture(t)
	d := operation.Dispatcher{Service: s, Factories: map[string]operation.MutationFactory{"fixture/compute": func(context.Context, provider.Binding) (provider.PowerMutation, error) {
		return mutationClient{f.server.URL}, nil
	}}}
	if err := operation.SetRecovery(context.Background(), pool, true, 1); err != nil {
		t.Fatal(err)
	}
	// Force due eligibility without depending on host/VM clock alignment.
	if _, err := pool.Exec(context.Background(), `UPDATE jobs SET next_run_at=clock_timestamp()-interval '1 second' WHERE operation_id=$1`, accepted.ID); err != nil {
		t.Fatal(err)
	}
	lease, err := (job.Scheduler{Pool: pool, Now: time.Now, Operations: true}).Claim(context.Background(), entityID(t))
	if err != nil {
		t.Fatal(err)
	}
	lease.Operation = accepted.ID
	return d, pool, lease, actor, target, f, pre
}

func claimSuccessor(t *testing.T, pool *pgxpool.Pool, old job.Lease) job.Lease {
	t.Helper()
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `UPDATE jobs SET next_run_at=clock_timestamp()-interval '1 second',lease_expires_at=CASE WHEN state='running' THEN clock_timestamp()-interval '1 second' ELSE NULL END WHERE id=$1`, old.ID); err != nil {
		t.Fatal(err)
	}
	lease, err := (job.Scheduler{Pool: pool, Now: time.Now, Operations: true}).Claim(ctx, entityID(t))
	if err != nil {
		t.Fatal(err)
	}
	lease.Operation = old.Operation
	return lease
}

func assertOperation(t *testing.T, pool *pgxpool.Pool, id uuid.UUID, state string, reserved bool) {
	t.Helper()
	var got string
	var locked bool
	err := pool.QueryRow(context.Background(), `SELECT state,EXISTS(SELECT 1 FROM resource_locks WHERE operation_id=o.id) FROM operations o WHERE id=$1`, id).Scan(&got, &locked)
	if err != nil || got != state || locked != reserved {
		t.Fatalf("state=%s reserved=%t want %s/%t error=%v", got, locked, state, reserved, err)
	}
}

func TestDispatchRevalidatesAuthorityAndFacts(t *testing.T) {
	for _, change := range []string{"membership", "disabled", "credential", "rotation", "grant", "facts", "policy"} {
		t.Run(change, func(t *testing.T) {
			d, pool, lease, actor, _, f, pre := dispatchSetup(t)
			ctx := context.Background()
			var err error
			switch change {
			case "membership":
				_, err = pool.Exec(ctx, `DELETE FROM memberships WHERE user_id=$1`, actor)
			case "disabled":
				_, err = pool.Exec(ctx, `UPDATE connections SET status='disabled' WHERE id=$1`, lease.Connection)
			case "credential":
				_, err = pool.Exec(ctx, `UPDATE credential_versions SET revoked_at=clock_timestamp() WHERE connection_id=$1`, lease.Connection)
			case "rotation":
				_, err = pool.Exec(ctx, `INSERT INTO credential_versions(workspace_id,connection_id,version,ciphertext,nonce,wrapped_data_key,wrapping_nonce,encryption_format,master_key_id)
 SELECT workspace_id,connection_id,2,ciphertext,nonce,wrapped_data_key,wrapping_nonce,encryption_format,master_key_id FROM credential_versions WHERE connection_id=$1 AND version=1`, lease.Connection)
				if err == nil {
					_, err = pool.Exec(ctx, `UPDATE connections SET active_credential_version=2 WHERE id=$1`, lease.Connection)
				}
			case "grant":
				_, err = pool.Exec(ctx, `UPDATE connection_grants SET actions=ARRAY['read'] WHERE user_id=$1`, actor)
			case "facts":
				pre.Fingerprint = sha256.Sum256([]byte("changed"))
			case "policy":
				d.Service.WithPolicy(operation.NewPolicy(nil, nil))
			}
			if err != nil {
				t.Fatal(err)
			}
			if err = d.Work(ctx, lease); err != nil {
				t.Fatal(err)
			}
			if f.mutations.Load() != 0 {
				t.Fatal("unsent authority failure mutated provider")
			}
			assertOperation(t, pool, lease.Operation, "failed", false)
		})
	}
}

func TestDispatchReceiptAndObservationAreAtomicAndNeverResubmit(t *testing.T) {
	d, pool, lease, _, _, f, _ := dispatchSetup(t)
	ctx := context.Background()
	if err := d.Work(ctx, lease); err != nil {
		t.Fatal(err)
	}
	assertOperation(t, pool, lease.Operation, "waiting_provider", true)
	f.mu.Lock()
	f.native = false
	f.mu.Unlock()
	if err := d.Work(ctx, claimSuccessor(t, pool, lease)); err != nil {
		t.Fatal(err)
	}
	assertOperation(t, pool, lease.Operation, "reconciling", true)
	f.mu.Lock()
	f.native = true
	f.mu.Unlock()
	if err := d.Work(ctx, claimSuccessor(t, pool, lease)); err != nil {
		t.Fatal(err)
	}
	assertOperation(t, pool, lease.Operation, "succeeded", false)
	if f.mutations.Load() != 1 || f.observations.Load() != 2 {
		t.Fatal("polling repeated mutation")
	}
	var audits int
	if pool.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE event_type='operation.completed'`).Scan(&audits) != nil || audits != 1 {
		t.Fatal("completion audit missing")
	}
}

func TestDispatchLostResponseAndExhaustedBudgetStayUnknown(t *testing.T) {
	d, pool, lease, _, _, f, _ := dispatchSetup(t)
	ctx := context.Background()
	f.mu.Lock()
	f.lose = true
	f.mu.Unlock()
	if err := d.Work(ctx, lease); err != nil {
		t.Fatal(err)
	}
	assertOperation(t, pool, lease.Operation, "reconciling", true)
	if _, err := pool.Exec(ctx, `UPDATE jobs SET deadline=created_at+interval '1 millisecond',next_run_at=created_at WHERE id=$1`, lease.ID); err != nil {
		t.Fatal(err)
	}
	if err := operation.Expire(ctx, pool); err != nil {
		t.Fatal(err)
	}
	assertOperation(t, pool, lease.Operation, "unknown", true)
	if f.mutations.Load() != 1 {
		t.Fatal("ambiguous call retried")
	}
}
