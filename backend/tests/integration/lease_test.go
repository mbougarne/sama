//go:build integration

package integration

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"sama/backend/internal/job"
)

func TestLeaseClaimsFenceStaleTokens(t *testing.T) {
	pool := identityDatabase(t)
	_, scope := seedIdentity(t, pool)
	ctx := context.Background()
	connection := entityID(t)
	if _, err := pool.Exec(ctx, `INSERT INTO connections(id,workspace_id,provider,api_family,account_identity,label) VALUES($1,$2,'fixture','compute','account','Fixture')`, connection, scope); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	record := job.Record{ID: entityID(t), Workspace: scope, Connection: connection, Type: "connection_refresh", PayloadVersion: 1, Deadline: now.Add(time.Hour)}
	if err = job.Insert(ctx, tx, record); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	// Advance injected time past the database-generated enqueue timestamp.
	now = now.Add(time.Second)
	scheduler := job.Scheduler{Pool: pool, Now: func() time.Time { return now }}
	var wg sync.WaitGroup
	results := make(chan job.Lease, 2)
	failures := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			lease, err := scheduler.Claim(ctx, uuid.New())
			if err != nil {
				failures <- err
			} else {
				results <- lease
			}
		}()
	}
	wg.Wait()
	if len(results) != 1 || len(failures) != 1 || !errors.Is(<-failures, job.ErrNoJob) {
		t.Fatal("concurrent claim did not select one owner")
	}
	lease := <-results
	stale := lease
	stale.Token = uuid.New()
	if scheduler.Renew(ctx, stale) != job.ErrLease || scheduler.Finish(ctx, stale, "succeeded") != job.ErrLease {
		t.Fatal("stale token accepted")
	}
	if err = scheduler.Renew(ctx, lease); err != nil {
		t.Fatal(err)
	}
	now = now.Add(62 * time.Second)
	replacement, err := scheduler.Claim(ctx, uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	if replacement.Token == lease.Token || scheduler.Finish(ctx, lease, "succeeded") != job.ErrLease {
		t.Fatal("old owner transitioned reclaimed work")
	}
	if err = scheduler.Finish(ctx, replacement, "succeeded"); err != nil {
		t.Fatal(err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err = scheduler.Claim(cancelled, uuid.New()); err != job.ErrLease {
		t.Fatal("unavailable database permitted claim", err)
	}
}
