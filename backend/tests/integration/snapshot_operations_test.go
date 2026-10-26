//go:build integration

package integration

import (
	"context"
	"crypto/sha256"
	"strconv"
	"testing"
	"time"

	"sama/backend/internal/identity"
	"sama/backend/internal/inventory"
	"sama/backend/internal/job"
	"sama/backend/internal/operation"
	"sama/backend/internal/provider"
)

type snapshotWorkerClient struct{ mutationClient }

func (c snapshotWorkerClient) Submit(ctx context.Context, b provider.Binding, action, id string, hash [32]byte) (provider.Receipt, error) {
	receipt, err := c.mutationClient.Submit(ctx, b, action, id, hash)
	if err == nil {
		receipt.Reference = "42/image-1"
		if action == provider.SnapshotDelete {
			receipt.Reference = "delete/42/image-1"
		}
	}
	return receipt, err
}
func TestSnapshotCanonicalReviewReservationAndRecovery(t *testing.T) {
	for _, action := range []string{provider.SnapshotCreate, provider.SnapshotDelete} {
		t.Run(action, func(t *testing.T) {
			pool, scope, actor, server := operationFixture(t)
			ctx := context.Background()
			run, image := entityID(t), entityID(t)
			if _, err := pool.Exec(ctx, `INSERT INTO sync_runs(id,workspace_id,connection_id,resource_type,scope,generation,status,scan_complete,completed_at) VALUES($1,$2,$3,'storage.snapshot','global',$1,'succeeded',true,clock_timestamp())`, run, scope.Workspace, scope.Connection); err != nil {
				t.Fatal(err)
			}
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			image, err = inventory.Upsert(ctx, tx, inventory.Observation{ID: image, Workspace: scope.Workspace, Connection: scope.Connection, ResourceType: inventory.SnapshotType, Family: "compute", Scope: "global", NativeID: "operation-target", Status: "available", Name: "Safe", Generation: run, ObservedAt: time.Now(), Details: inventory.Details{Snapshot: &inventory.SnapshotDetails{Regions: []string{}}}})
			if err != nil || tx.Commit(ctx) != nil {
				t.Fatal("snapshot target", err)
			}
			if _, err = pool.Exec(ctx, `UPDATE connections SET verified_capabilities=ARRAY['compute.server.snapshot_create','storage.snapshot.delete'],permissions_verified_at=clock_timestamp() WHERE id=$1`, scope.Connection); err != nil {
				t.Fatal(err)
			}
			if _, err = pool.Exec(ctx, `UPDATE connection_grants SET actions=ARRAY['read','operate','high_impact'] WHERE user_id=$1`, actor); err != nil {
				t.Fatal(err)
			}
			target, other, kind := server, image, "compute.server"
			if action == provider.SnapshotDelete {
				target, other, kind = image, server, inventory.SnapshotType
			}
			stamp := 1
			key := "fixture/compute:" + action
			sessions, _ := identity.NewSessions(pool, identity.DefaultSessionPolicy(), time.Now)
			service := operation.NewService(pool, sessions, true, map[string]operation.ReviewFactory{key: func(_ context.Context, b provider.Binding) (provider.PowerReviewer, error) {
				if b.Connection() != scope.Connection || b.Version() != 1 {
					t.Fatal("review binding")
				}
				return powerReader{func(_ context.Context, _ provider.Binding, got, native string) (provider.Preconditions, error) {
					if got != action || native != "operation-target" {
						t.Fatal("review intent changed")
					}
					return provider.Preconditions{Fingerprint: sha256.Sum256([]byte(action + strconv.Itoa(stamp))), TargetName: "Safe", Allowed: true, HighImpact: true}, nil
				}}, nil
			}}).WithPolicy(operation.NewPolicy([]string{key}, map[string]bool{key: true}))
			principal := identity.Principal{UserID: actor, AuthenticatedAt: time.Now(), Reauthenticated: true}
			intent := operation.Intent{Action: action, Parameters: &operation.PowerParameters{}}
			if _, err = service.Prepare(ctx, principal, scope.Workspace, other, intent); err != operation.ErrDenied {
				t.Fatal("wrong target type", err)
			}
			stale := principal
			stale.Reauthenticated = false
			if _, err = service.Prepare(ctx, stale, scope.Workspace, target, intent); err != identity.ErrRecentRequired {
				t.Fatal("recent authentication bypass", err)
			}
			review, err := service.Prepare(ctx, principal, scope.Workspace, target, intent)
			if err != nil {
				t.Fatal(err)
			}
			confirm := operation.Confirmation{ReviewID: review.ID, Confirmed: true}
			if _, err = service.Confirm(ctx, principal, scope.Workspace, "snapshot-key", confirm, "snapshot-test"); err != operation.ErrInvalid {
				t.Fatal("typed target bypass", err)
			}
			confirm.TypedTarget = "Safe"
			stamp++
			if _, err = service.Confirm(ctx, principal, scope.Workspace, "snapshot-key", confirm, "snapshot-test"); err != operation.ErrStale {
				t.Fatal("stale snapshot accepted", err)
			}
			stamp--
			accepted, err := service.Confirm(ctx, principal, scope.Workspace, "snapshot-key", confirm, "snapshot-test")
			if err != nil {
				t.Fatal(err)
			}
			again, err := service.Confirm(ctx, principal, scope.Workspace, "snapshot-key", confirm, "snapshot-test")
			if err != nil || again.ID != accepted.ID {
				t.Fatal("snapshot acceptance duplicated", err)
			}
			var lockedType string
			if pool.QueryRow(ctx, `SELECT resource_type FROM resource_locks WHERE operation_id=$1`, accepted.ID).Scan(&lockedType) != nil || lockedType != kind {
				t.Fatal("wrong reservation type", lockedType)
			}
			competing, err := service.Prepare(ctx, principal, scope.Workspace, target, intent)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = service.Confirm(ctx, principal, scope.Workspace, "competing-key", operation.Confirmation{ReviewID: competing.ID, Confirmed: true, TypedTarget: "Safe"}, "snapshot-test"); err != operation.ErrBusy {
				t.Fatal("snapshot conflict allowed", err)
			}
			fixture := newMutationFixture(t)
			fixture.lose = action == provider.SnapshotCreate
			dispatcher := operation.Dispatcher{Service: service, Factories: map[string]operation.MutationFactory{key: func(context.Context, provider.Binding) (provider.PowerMutation, error) {
				return snapshotWorkerClient{mutationClient{fixture.server.URL}}, nil
			}}}
			if err = operation.SetRecovery(ctx, pool, true, 1); err != nil {
				t.Fatal(err)
			}
			if _, err = pool.Exec(ctx, `UPDATE jobs SET next_run_at=clock_timestamp()-interval '1 second' WHERE operation_id=$1`, accepted.ID); err != nil {
				t.Fatal(err)
			}
			lease, err := (job.Scheduler{Pool: pool, Now: time.Now, Operations: true}).Claim(ctx, entityID(t))
			if err != nil {
				t.Fatal(err)
			}
			lease.Operation = accepted.ID
			if err = dispatcher.Work(ctx, lease); err != nil {
				t.Fatal(err)
			}
			want := "waiting_provider"
			if fixture.lose {
				want = "reconciling"
			}
			assertOperation(t, pool, accepted.ID, want, true)
			var reference *string
			if pool.QueryRow(ctx, `SELECT provider_reference FROM operations WHERE id=$1`, accepted.ID).Scan(&reference) != nil || !fixture.lose && reference == nil || fixture.lose && reference != nil {
				t.Fatal("composite receipt not stored")
			}
			fixture.mu.Lock()
			fixture.outcome = "unknown"
			fixture.native = false
			fixture.mu.Unlock()
			successor := claimSuccessor(t, pool, lease)
			if err = dispatcher.Work(ctx, successor); err != nil {
				t.Fatal(err)
			}
			assertOperation(t, pool, accepted.ID, "unknown", true)
			if fixture.mutations.Load() != 1 {
				t.Fatal("unknown replayed mutation")
			}
			fixture.mu.Lock()
			fixture.outcome = "succeeded"
			fixture.native = true
			fixture.mu.Unlock()
			if _, err = service.Recheck(ctx, principal, scope.Workspace, accepted.ID, "snapshot-recheck"); err != nil {
				t.Fatal(err)
			}
			successor = claimSuccessor(t, pool, successor)
			if err = dispatcher.Work(ctx, successor); err != nil {
				t.Fatal(err)
			}
			assertOperation(t, pool, accepted.ID, "succeeded", false)
			if fixture.mutations.Load() != 1 || fixture.observations.Load() != 2 {
				t.Fatal("reconciliation submitted twice")
			}
			if _, err = pool.Exec(ctx, `UPDATE connection_grants SET actions=ARRAY['read'] WHERE user_id=$1`, actor); err != nil {
				t.Fatal(err)
			}
			if _, err = service.Prepare(ctx, principal, scope.Workspace, target, intent); err != operation.ErrDenied {
				t.Fatal("revoked snapshot authority", err)
			}
			detail, err := inventory.Detail(ctx, pool, actor, scope.Workspace, target, map[string][]string{"fixture/compute": {action}})
			if err != nil {
				t.Fatal(err)
			}
			for _, capability := range detail.AvailableActions {
				if capability.Action == action && capability.Available {
					t.Fatal("revoked snapshot action advertised")
				}
			}
		})
	}
}
