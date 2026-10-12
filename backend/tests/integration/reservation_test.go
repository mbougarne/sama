//go:build integration

package integration

import (
	"context"
	"sync"
	"testing"

	"github.com/google/uuid"
	"sama/backend/internal/operation"
)

func TestReservationsSerializeTargetsRetainUnknownAndRejectMultiResource(t *testing.T) {
	pool, lease, actor, target := operationFixture(t)
	first := storedOperation(t, pool, lease, actor, storedReview(t, pool, lease, actor, target), target, "first-reservation")
	second := storedOperation(t, pool, lease, actor, storedReview(t, pool, lease, actor, target), target, "second-reservation")
	ctx := context.Background()
	results := make(chan uuid.UUID, 2)
	failures := make(chan error, 2)
	var wg sync.WaitGroup
	for _, id := range []uuid.UUID{first, second} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tx, err := pool.Begin(ctx)
			if err != nil {
				failures <- err
				return
			}
			defer tx.Rollback(ctx)
			_, err = operation.Reserve(ctx, tx, lease.Workspace, lease.Connection, id, []operation.Target{{Kind: "resource", Scope: "global", NativeID: "operation-target"}})
			if err != nil {
				failures <- err
				return
			}
			if tx.Commit(ctx) != nil {
				failures <- operation.ErrStore
				return
			}
			results <- id
		}()
	}
	wg.Wait()
	if len(results) != 1 || len(failures) != 1 || <-failures != operation.ErrBusy {
		t.Fatal("both operators reserved target")
	}
	owner := <-results
	if _, err := pool.Exec(ctx, `UPDATE operations SET state='unknown' WHERE id=$1`, owner); err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if operation.ReleaseTerminal(ctx, tx, lease.Workspace, owner) != operation.ErrState {
		t.Fatal("unknown reservation released")
	}
	tx.Rollback(ctx)
	var count int
	if pool.QueryRow(ctx, `SELECT count(*) FROM resource_locks WHERE operation_id=$1`, owner).Scan(&count) != nil || count != 1 {
		t.Fatal("unknown lost lock")
	}
	tx, err = pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = operation.Reserve(ctx, tx, lease.Workspace, lease.Connection, second, []operation.Target{{}, {}}); err != operation.ErrInvalid {
		t.Fatal("multi-resource admitted")
	}
	tx.Rollback(ctx)
	tx, err = pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `UPDATE operations SET state='failed' WHERE id=$1`, owner); err != nil || operation.ReleaseTerminal(ctx, tx, lease.Workspace, owner) != nil || tx.Commit(ctx) != nil {
		t.Fatal("terminal release failed")
	}
	if pool.QueryRow(ctx, `SELECT count(*) FROM resource_locks WHERE operation_id=$1`, owner).Scan(&count) != nil || count != 0 {
		t.Fatal("terminal lock retained")
	}
}
