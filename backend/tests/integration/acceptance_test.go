//go:build integration

package integration

import (
	"context"
	"crypto/sha256"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"sama/backend/internal/identity"
	"sama/backend/internal/job"
	"sama/backend/internal/operation"
	"sama/backend/internal/provider"
)

func acceptanceSetup(t *testing.T, hooks ...func(*pgxpool.Pool, job.Lease, uuid.UUID)) (*operation.Service, *pgxpool.Pool, job.Lease, uuid.UUID, uuid.UUID, *provider.Preconditions, *time.Time) {
	t.Helper()
	pool, lease, actor, target := operationFixture(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `UPDATE connections SET verified_capabilities=ARRAY['compute.server.power_on'],permissions_verified_at=clock_timestamp() WHERE id=$1`, lease.Connection); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE connection_grants SET actions=ARRAY['read','operate','high_impact'] WHERE user_id=$1`, actor); err != nil {
		t.Fatal(err)
	}
	pre := &provider.Preconditions{Fingerprint: sha256.Sum256([]byte("stopped")), TargetName: "Safe", Allowed: true}
	now := time.Now()
	sessions, _ := identity.NewSessions(pool, identity.DefaultSessionPolicy(), time.Now)
	service := operation.NewService(pool, sessions, true, map[string]operation.ReviewFactory{"fixture/compute": func(_ context.Context, b provider.Binding) (provider.PowerReviewer, error) {
		if b.Connection() != lease.Connection || b.Workspace() != lease.Workspace || b.Version() != 1 {
			t.Fatal("review binding escaped")
		}
		return powerReader{func(context.Context, provider.Binding, string, string) (provider.Preconditions, error) {
			if len(hooks) > 0 && hooks[0] != nil {
				hooks[0](pool, lease, actor)
			}
			return *pre, nil
		}}, nil
	}}, func() time.Time { return now })
	return service, pool, lease, actor, target, pre, &now
}

func prepareAcceptance(t *testing.T, s *operation.Service, actor, scope, target uuid.UUID) operation.Confirmation {
	t.Helper()
	review, err := s.Prepare(context.Background(), identity.Principal{UserID: actor}, scope, target, operation.Intent{Action: "compute.server.power_on", Parameters: &operation.PowerParameters{}})
	if err != nil {
		t.Fatal(err)
	}
	return operation.Confirmation{ReviewID: review.ID, Confirmed: true}
}

func TestConcurrentAcceptanceDeduplicatesAndRecoversConsumedExpiredReview(t *testing.T) {
	s, pool, lease, actor, target, _, now := acceptanceSetup(t)
	c := prepareAcceptance(t, s, actor, lease.Workspace, target)
	ctx := context.Background()
	results := make(chan operation.Accepted, 8)
	failures := make(chan error, 8)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := s.Confirm(ctx, identity.Principal{UserID: actor}, lease.Workspace, "same-key", c, "accept-test")
			if err != nil {
				failures <- err
			} else {
				results <- result
			}
		}()
	}
	wg.Wait()
	close(results)
	if len(failures) != 0 || len(results) != 8 {
		t.Fatal("concurrent confirmation failed")
	}
	var id uuid.UUID
	for result := range results {
		if id == uuid.Nil {
			id = result.ID
		}
		if result.ID != id {
			t.Fatal("duplicate operation")
		}
	}
	for _, query := range []string{`SELECT count(*) FROM operations`, `SELECT count(*) FROM resource_locks`, `SELECT count(*) FROM jobs WHERE operation_id IS NOT NULL`, `SELECT count(*) FROM operation_events`, `SELECT count(*) FROM audit_events WHERE event_type='operation.accepted'`} {
		var count int
		if pool.QueryRow(ctx, query).Scan(&count) != nil || count != 1 {
			t.Fatal("non-atomic duplicate effects")
		}
	}
	*now = now.Add(6 * time.Minute)
	recovered, err := s.Confirm(ctx, identity.Principal{UserID: actor}, lease.Workspace, "same-key", c, "recover-test")
	if err != nil || recovered.ID != id {
		t.Fatal("lost response not recovered", err)
	}
	if _, err = s.Confirm(ctx, identity.Principal{UserID: actor}, lease.Workspace, "new-key", c, "expired-test"); err != operation.ErrStale {
		t.Fatal("expired/consumed review accepted", err)
	}
}

func TestAcceptanceRollsBackAuditFailureAndRejectsChangedPreconditions(t *testing.T) {
	s, pool, lease, actor, target, pre, _ := acceptanceSetup(t)
	c := prepareAcceptance(t, s, actor, lease.Workspace, target)
	ctx := context.Background()
	original := pre.Fingerprint
	pre.Fingerprint = sha256.Sum256([]byte("running"))
	if _, err := s.Confirm(ctx, identity.Principal{UserID: actor}, lease.Workspace, "stale-key", c, "stale-test"); err != operation.ErrStale {
		t.Fatal("changed facts accepted", err)
	}
	pre.Fingerprint = original
	if _, err := pool.Exec(ctx, `CREATE FUNCTION reject_accept_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.event_type='operation.accepted' THEN RAISE EXCEPTION 'synthetic audit failure'; END IF; RETURN NEW; END $$;
 CREATE TRIGGER reject_accept_audit BEFORE INSERT ON audit_events FOR EACH ROW EXECUTE FUNCTION reject_accept_audit()`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Confirm(ctx, identity.Principal{UserID: actor}, lease.Workspace, "atomic-key", c, "atomic-test"); err == nil {
		t.Fatal("audit failure accepted")
	}
	for _, query := range []string{`SELECT count(*) FROM operations`, `SELECT count(*) FROM resource_locks`, `SELECT count(*) FROM jobs WHERE operation_id IS NOT NULL`, `SELECT count(*) FROM operation_events`, `SELECT count(*) FROM action_reviews WHERE consumed_by IS NOT NULL`} {
		var count int
		if pool.QueryRow(ctx, query).Scan(&count) != nil || count != 0 {
			t.Fatal("failed acceptance leaked rows")
		}
	}
	if _, err := pool.Exec(ctx, `DROP TRIGGER reject_accept_audit ON audit_events`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Confirm(ctx, identity.Principal{UserID: actor}, lease.Workspace, "atomic-key", c, "atomic-test"); err != nil {
		t.Fatal("rollback consumed review", err)
	}
}
