//go:build integration

package integration

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"sama/backend/internal/job"
)

func TestLeaseRunChecksOwnershipAndInjectedDeadline(t *testing.T) {
	pool := identityDatabase(t)
	_, workspace := seedIdentity(t, pool)
	ctx := context.Background()
	connection := entityID(t)
	if _, err := pool.Exec(ctx, `INSERT INTO connections(id,workspace_id,provider,api_family,account_identity,label) VALUES($1,$2,'fixture','compute','account','Fixture')`, connection, workspace); err != nil {
		t.Fatal(err)
	}
	// A distant injected clock must still yield a short real execution budget.
	now := time.Now().Add(24 * time.Hour)
	record := job.Record{ID: entityID(t), Workspace: workspace, Connection: connection, Type: "connection_refresh", PayloadVersion: 1, Deadline: now.Add(250 * time.Millisecond)}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if err = job.Insert(ctx, tx, record); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	scheduler := job.Scheduler{Pool: pool, Now: func() time.Time { return now }}
	lease, err := scheduler.Claim(ctx, uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	stale := lease
	stale.Token = uuid.New()
	if err = scheduler.Run(ctx, stale, func(context.Context) error {
		t.Error("stale token started work")
		return nil
	}); err != job.ErrLease {
		t.Fatal(err)
	}
	called := false
	started := time.Now()
	bounded, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	err = scheduler.Run(bounded, lease, func(ctx context.Context) error {
		called = true
		<-ctx.Done()
		return ctx.Err()
	})
	if !called || err == nil || time.Since(started) > time.Second {
		t.Fatal("work was not bounded by the injected job deadline", called, err)
	}
	if _, err = pool.Exec(ctx, `UPDATE jobs SET lease_expires_at=$2 WHERE id=$1`, lease.ID, now.Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	if err = scheduler.Run(ctx, lease, func(context.Context) error {
		t.Error("expired lease started work")
		return nil
	}); err != job.ErrLease {
		t.Fatal(err)
	}
}
