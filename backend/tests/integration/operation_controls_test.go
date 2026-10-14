//go:build integration

package integration

import (
	"context"
	"sync"
	"testing"
	"time"

	"sama/backend/internal/identity"
	"sama/backend/internal/job"
	"sama/backend/internal/operation"
)

func TestOperationCancelHTTPAndDispatchBoundary(t *testing.T) {
	for _, submitted := range []bool{false, true} {
		t.Run(map[bool]string{false: "queued", true: "submitted"}[submitted], func(t *testing.T) {
			d, pool, lease, actor, _, f, _ := dispatchSetup(t)
			request := operationHTTP(t, pool, actor, d.Service)
			path := "/api/v1/workspaces/" + lease.Workspace.String() + "/operations/" + lease.Operation.String() + "/cancel"
			if request(path, "{}", "", false).Code != 403 || request(path, `{"unexpected":true}`, "", true).Code != 400 {
				t.Fatal("mutation boundary bypass")
			}
			if request("/api/v1/workspaces/"+lease.Workspace.String()+"/operations/"+entityID(t).String()+"/cancel", "{}", "", true).Code != 404 {
				t.Fatal("unscoped target exposed")
			}
			if submitted {
				if err := d.Work(context.Background(), lease); err != nil {
					t.Fatal(err)
				}
			}
			response := request(path, "{}", "", true)
			if submitted {
				if response.Code != 409 {
					t.Fatal("submitted cancellation accepted", response.Code)
				}
			} else {
				if response.Code != 200 {
					t.Fatal("queued cancellation failed", response.Code, response.Body.String())
				}
				assertOperation(t, pool, lease.Operation, "cancelled", false)
				if d.Work(context.Background(), lease) == nil || f.mutations.Load() != 0 {
					t.Fatal("cancelled lease sent")
				}
			}
		})
	}
}

func TestOperationRechecksDeduplicateAndRetainReservation(t *testing.T) {
	d, pool, lease, actor, _, f, _ := dispatchSetup(t)
	ctx := context.Background()
	f.mu.Lock()
	f.outcome = "unknown"
	f.mu.Unlock()
	if err := d.Work(ctx, lease); err != nil {
		t.Fatal(err)
	}
	if err := d.Work(ctx, claimSuccessor(t, pool, lease)); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errors := make(chan error, 6)
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := d.Service.Recheck(ctx, identity.Principal{UserID: actor}, lease.Workspace, lease.Operation, "recheck-test")
			errors <- err
		}()
	}
	wg.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	assertOperation(t, pool, lease.Operation, "reconciling", true)
	var count int
	if pool.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE metadata->>'reason_code'='user_requested_recheck'`).Scan(&count) != nil || count != 1 {
		t.Fatal("recheck not coalesced")
	}
	if err := d.Work(ctx, claimSuccessor(t, pool, lease)); err != nil {
		t.Fatal(err)
	}
	if f.mutations.Load() != 1 {
		t.Fatal("recheck submitted again")
	}
	if _, err := pool.Exec(ctx, `UPDATE connection_grants SET actions=ARRAY['read'] WHERE user_id=$1`, actor); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Service.Recheck(ctx, identity.Principal{UserID: actor}, lease.Workspace, lease.Operation, "denied-recheck"); err != operation.ErrDenied {
		t.Fatal("viewer recheck accepted", err)
	}
}

func TestConcurrentCancellationAndDispatchHaveOneOutcome(t *testing.T) {
	d, pool, lease, actor, _, f, _ := dispatchSetup(t)
	ctx := context.Background()
	start := make(chan struct{})
	done := make(chan error, 2)
	go func() { <-start; done <- d.Work(ctx, lease) }()
	go func() {
		<-start
		_, err := d.Service.Cancel(ctx, identity.Principal{UserID: actor}, lease.Workspace, lease.Operation, "cancel-race")
		done <- err
	}()
	close(start)
	for i := 0; i < 2; i++ {
		err := <-done
		if err != nil && err != operation.ErrState && err != job.ErrLease {
			t.Fatal(err)
		}
	}
	var state string
	var events int
	if pool.QueryRow(ctx, `SELECT state,(SELECT count(*) FROM audit_events WHERE event_type='operation.cancelled' AND metadata->>'operation_id'=o.id::text) FROM operations o WHERE id=$1`, lease.Operation).Scan(&state, &events) != nil {
		t.Fatal("race outcome unavailable")
	}
	if state == "cancelled" {
		if f.mutations.Load() != 0 || events != 1 {
			t.Fatal("cancelled work sent or unaudited")
		}
		assertOperation(t, pool, lease.Operation, "cancelled", false)
	} else if state == "waiting_provider" {
		if f.mutations.Load() != 1 || events != 0 {
			t.Fatal("dispatch race produced extra effects")
		}
		assertOperation(t, pool, lease.Operation, "waiting_provider", true)
	} else {
		t.Fatal("invalid race outcome", state)
	}
}

func TestOwnerResolutionPreservesUnknownAndRequiresRecentRiskReview(t *testing.T) {
	d, pool, lease, actor, _, f, _ := dispatchSetup(t)
	ctx := context.Background()
	f.mu.Lock()
	f.outcome = "unknown"
	f.mu.Unlock()
	if err := d.Work(ctx, lease); err != nil {
		t.Fatal(err)
	}
	if err := d.Work(ctx, claimSuccessor(t, pool, lease)); err != nil {
		t.Fatal(err)
	}
	p := identity.Principal{UserID: actor, Reauthenticated: true, AuthenticatedAt: time.Now().Add(-time.Second)}
	input := operation.Resolution{Acknowledged: true, Target: "Safe"}
	if _, err := d.Service.ReleaseUnknown(ctx, identity.Principal{UserID: actor}, lease.Workspace, lease.Operation, input, "resolution-test"); err != identity.ErrRecentRequired {
		t.Fatal("stale authentication accepted", err)
	}
	if _, err := d.Service.ReleaseUnknown(ctx, p, lease.Workspace, lease.Operation, operation.Resolution{Target: "Safe"}, "resolution-test"); err != operation.ErrInvalid {
		t.Fatal("risk not acknowledged", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE memberships SET role='admin' WHERE user_id=$1`, actor); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Service.ReleaseUnknown(ctx, p, lease.Workspace, lease.Operation, input, "resolution-test"); err != operation.ErrDenied {
		t.Fatal("nonowner release", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE memberships SET role='owner' WHERE user_id=$1`, actor); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Service.ReleaseUnknown(ctx, p, lease.Workspace, lease.Operation, input, "resolution-test"); err != nil {
		t.Fatal(err)
	}
	assertOperation(t, pool, lease.Operation, "unknown", false)
	if _, err := d.Service.Recheck(ctx, p, lease.Workspace, lease.Operation, "after-release"); err != operation.ErrState {
		t.Fatal("released intent requeued", err)
	}
	view, err := operation.Read(ctx, pool, actor, lease.Workspace, lease.Operation, "", 1)
	if err != nil || !view.Data[0].ReservationReleased || !view.Data[0].Uncertain {
		t.Fatal("resolution evidence hidden", err)
	}
	if f.mutations.Load() != 1 {
		t.Fatal("release resubmitted")
	}
}
