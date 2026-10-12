//go:build integration

package integration

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"sama/backend/internal/inventory"
	"sama/backend/internal/job"
	"sama/backend/internal/provider"
)

func TestRegisteredSchedulerExcludesSecondReplicaAndRecoversInterruptedScan(t *testing.T) {
	pool, lease, _, run := inventoryLease(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `UPDATE jobs SET state='queued',lease_owner=NULL,lease_token=NULL,lease_expires_at=NULL,next_run_at=clock_timestamp()-interval '1 second' WHERE id=$1`, lease.ID); err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{}, 1)
	var calls atomic.Int32
	scanner := inventory.Scanner{Pool: pool, Reader: func(context.Context, provider.Binding) (provider.InventoryReader, error) {
		return scanReader{func(ctx context.Context, _ string) (provider.InventoryPage, error) {
			calls.Add(1)
			entered <- struct{}{}
			<-ctx.Done()
			return provider.InventoryPage{}, ctx.Err()
		}}, nil
	}}
	scheduler := job.Scheduler{Pool: pool, Now: time.Now}
	runner, err := job.NewRunner(scheduler, 4, 2*time.Second, map[job.Key]job.Handler{{Type: "connection_refresh", Version: 2}: {Work: scanner.Scan, Finish: scanner.Complete}}, func(ctx context.Context, l job.Lease) error { return scheduler.Finish(ctx, l, "failed") })
	if err != nil {
		t.Fatal(err)
	}
	firstCtx, stop := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- runner.Run(firstCtx) }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		stop()
		t.Fatal("queued sync never started")
	}
	if err := runner.Run(ctx); !errors.Is(err, job.ErrActive) {
		stop()
		t.Fatal("second scheduler active", err)
	}
	stop()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("shutdown hung")
	}
	var state string
	if pool.QueryRow(ctx, `SELECT state FROM jobs WHERE id=$1`, lease.ID).Scan(&state) != nil || state != "running" {
		t.Fatal("shutdown discarded recoverable job")
	}
	if _, err := pool.Exec(ctx, `UPDATE jobs SET lease_expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`, lease.ID); err != nil {
		t.Fatal(err)
	}
	scanner.Reader = func(context.Context, provider.Binding) (provider.InventoryReader, error) {
		return scanReader{func(context.Context, string) (provider.InventoryPage, error) {
			calls.Add(1)
			return provider.InventoryPage{Data: []provider.Observation{scanObservation("restart")}}, nil
		}}, nil
	}
	runner, err = job.NewRunner(scheduler, 4, 2*time.Second, map[job.Key]job.Handler{{Type: "connection_refresh", Version: 2}: {Work: scanner.Scan, Finish: scanner.Complete}}, func(ctx context.Context, l job.Lease) error { return scheduler.Finish(ctx, l, "failed") })
	if err != nil {
		t.Fatal(err)
	}
	nextCtx, cancel := context.WithCancel(ctx)
	go func() { done <- runner.Run(nextCtx) }()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if pool.QueryRow(ctx, `SELECT status FROM sync_runs WHERE id=$1`, run).Scan(&state) == nil && state == "succeeded" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	if err := <-done; err != nil || state != "succeeded" || calls.Load() != 2 {
		t.Fatal("restart did not complete", err, state, calls.Load())
	}
}

func TestSchedulerNeverExecutesAnUnregisteredPayloadVersion(t *testing.T) {
	pool, lease, _, run := inventoryLease(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `UPDATE jobs SET state='queued',lease_owner=NULL,lease_token=NULL,lease_expires_at=NULL,next_run_at=clock_timestamp()-interval '1 second' WHERE id=$1`, lease.ID); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	scanner := inventory.Scanner{Pool: pool}
	runner, err := job.NewRunner(job.Scheduler{Pool: pool, Now: time.Now}, 1, time.Second, map[job.Key]job.Handler{
		{Type: "connection_refresh", Version: 99}: {Work: func(context.Context, job.Lease) error { calls.Add(1); return nil }, Finish: scanner.Complete},
	}, func(ctx context.Context, l job.Lease) error { return scanner.Complete(ctx, l, job.ErrJob) })
	if err != nil {
		t.Fatal(err)
	}
	work, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- runner.Run(work) }()
	deadline := time.Now().Add(5 * time.Second)
	state := ""
	for time.Now().Before(deadline) {
		pool.QueryRow(ctx, `SELECT status FROM sync_runs WHERE id=$1`, run).Scan(&state)
		if state == "failed" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	if err := <-done; err != nil || state != "failed" || calls.Load() != 0 {
		t.Fatal("unknown version executed", err, state, calls.Load())
	}
}
