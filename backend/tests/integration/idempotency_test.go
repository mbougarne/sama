//go:build integration

package integration

import (
	"context"
	"testing"

	"sama/backend/internal/operation"
)

func TestIdempotencyResolvesConsumedReviewsWithoutCrossActorDisclosure(t *testing.T) {
	pool, lease, actor, target := operationFixture(t)
	review := storedReview(t, pool, lease, actor, target)
	id := storedOperation(t, pool, lease, actor, review, target, "idempotency-key")
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `UPDATE action_reviews SET consumed_by=$2 WHERE id=$1`, review, id); err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	result, found, err := operation.Resolve(ctx, tx, lease.Workspace, actor, "idempotency-key", [32]byte{})
	if err != nil || !found || result.ID != id {
		t.Fatal("consumed review lost operation", err)
	}
	changed := [32]byte{1}
	if _, _, err = operation.Resolve(ctx, tx, lease.Workspace, actor, "idempotency-key", changed); err != operation.ErrConflict {
		t.Fatal("different intent reused key", err)
	}
	other, _ := seedIdentity(t, pool)
	if _, found, err = operation.Resolve(ctx, tx, lease.Workspace, other, "idempotency-key", [32]byte{}); err != nil || found {
		t.Fatal("another actor retrieved operation", err)
	}
	if operation.ValidKey("key\nsecret") || operation.ValidKey("") {
		t.Fatal("unsafe key accepted")
	}
}
