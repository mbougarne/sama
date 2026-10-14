//go:build integration

package integration

import (
	"context"
	"testing"
	"time"

	"sama/backend/internal/job"
	"sama/backend/internal/operation"
)

func TestReceiptDatabaseFailureRecoversOnlyByObservation(t *testing.T) {
	d, pool, lease, _, _, f, _ := dispatchSetup(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `CREATE FUNCTION reject_receipt() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.provider_reference IS NOT NULL THEN RAISE EXCEPTION 'synthetic receipt failure'; END IF; RETURN NEW; END $$;
 CREATE TRIGGER reject_receipt BEFORE UPDATE ON operations FOR EACH ROW EXECUTE FUNCTION reject_receipt()`); err != nil {
		t.Fatal(err)
	}
	if err := d.Work(ctx, lease); err == nil {
		t.Fatal("receipt persistence failure ignored")
	}
	assertOperation(t, pool, lease.Operation, "dispatching", true)
	if _, err := pool.Exec(ctx, `DROP TRIGGER reject_receipt ON operations`); err != nil {
		t.Fatal(err)
	}
	successor := claimSuccessor(t, pool, lease)
	if err := d.Work(ctx, successor); err != nil {
		t.Fatal(err)
	}
	assertOperation(t, pool, lease.Operation, "succeeded", false)
	if f.mutations.Load() != 1 || f.observations.Load() != 1 {
		t.Fatal("success-before-commit duplicated")
	}
	if err := d.Work(ctx, lease); err != job.ErrLease {
		t.Fatal("stale worker overwrote successor", err)
	}
}

func TestStalledWorkerAndFailedRenewalCannotOverwriteSuccessor(t *testing.T) {
	d, pool, lease, _, _, f, _ := dispatchSetup(t)
	ctx := context.Background()
	f.pause("/before-receipt")
	done := make(chan error, 1)
	go func() { done <- d.Work(ctx, lease) }()
	f.wait(t, "/before-receipt")
	successor := claimSuccessor(t, pool, lease)
	if err := (job.Scheduler{Pool: pool, Now: time.Now}).Renew(ctx, lease); err != job.ErrLease {
		t.Fatal("stale lease renewed", err)
	}
	if err := d.Work(ctx, successor); err != nil {
		t.Fatal(err)
	}
	f.resume("/before-receipt")
	if err := <-done; err != job.ErrLease {
		t.Fatal("stalled receipt overwrote successor", err)
	}
	assertOperation(t, pool, lease.Operation, "succeeded", false)
	if f.mutations.Load() != 1 {
		t.Fatal("stalled worker duplicated effect")
	}
}

func TestDispatchMarkerDatabaseFailurePreventsMutation(t *testing.T) {
	d, pool, lease, _, _, f, _ := dispatchSetup(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `CREATE FUNCTION reject_marker() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.state='dispatching' THEN RAISE EXCEPTION 'synthetic marker failure'; END IF; RETURN NEW; END $$;
 CREATE TRIGGER reject_marker BEFORE UPDATE ON operations FOR EACH ROW EXECUTE FUNCTION reject_marker()`); err != nil {
		t.Fatal(err)
	}
	if err := d.Work(ctx, lease); err == nil {
		t.Fatal("marker persistence failure ignored")
	}
	assertOperation(t, pool, lease.Operation, "queued", true)
	if f.mutations.Load() != 0 {
		t.Fatal("provider call preceded durable marker")
	}
}

func TestRestoreGateBlocksDispatchButAllowsReconciliation(t *testing.T) {
	d, pool, lease, _, _, f, _ := dispatchSetup(t)
	ctx := context.Background()
	if err := operation.SetRecovery(ctx, pool, false, 0); err != nil {
		t.Fatal(err)
	}
	if err := d.Work(ctx, lease); err != operation.ErrRecovery {
		t.Fatal("closed gate dispatched", err)
	}
	assertOperation(t, pool, lease.Operation, "queued", true)
	status, err := operation.Recovery(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	if err := operation.SetRecovery(ctx, pool, true, status.Version-1); err != operation.ErrStale {
		t.Fatal("uninspected gate version accepted", err)
	}
	if err := operation.SetRecovery(ctx, pool, true, status.Version); err != nil {
		t.Fatal(err)
	}
	if err := d.Work(ctx, lease); err != nil {
		t.Fatal(err)
	}
	if err := operation.SetRecovery(ctx, pool, false, 0); err != nil {
		t.Fatal(err)
	}
	status, err = operation.Recovery(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	if err := operation.SetRecovery(ctx, pool, true, status.Version); err != operation.ErrRecovery {
		t.Fatal("unresolved restored submission admitted", err)
	}
	if err := d.Work(ctx, claimSuccessor(t, pool, lease)); err != nil {
		t.Fatal(err)
	}
	assertOperation(t, pool, lease.Operation, "succeeded", false)
	if f.mutations.Load() != 1 {
		t.Fatal("restore gate resubmitted")
	}
}
