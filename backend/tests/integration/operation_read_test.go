//go:build integration

package integration

import (
	"context"
	"github.com/google/uuid"
	"sama/backend/internal/operation"
	"sama/backend/internal/workspace"
	"testing"
)

func TestOperationHistoryReadsCurrentAuthorityAndBoundedEvidence(t *testing.T) {
	pool, lease, actor, target := operationFixture(t)
	ctx := context.Background()
	id := storedOperation(t, pool, lease, actor, storedReview(t, pool, lease, actor, target), target, "read-one")
	other := storedOperation(t, pool, lease, actor, storedReview(t, pool, lease, actor, target), target, "read-two")
	if _, err := pool.Exec(ctx, `UPDATE operations SET state=CASE WHEN id=$1 THEN 'unknown' ELSE 'succeeded' END,provider_reference='safe-action-42',created_at='2026-10-01T00:00:00Z' WHERE workspace_id=$2`, id, lease.Workspace); err != nil {
		t.Fatal(err)
	}
	for i := int64(1); i <= 25; i++ {
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if operation.AppendEvent(ctx, tx, lease.Workspace, id, i, "reconciling", "unknown") != nil || tx.Commit(ctx) != nil {
			t.Fatal("event")
		}
	}
	detail, err := operation.Read(ctx, pool, actor, lease.Workspace, id, "", 1)
	if err != nil || len(detail.Data) != 1 || !detail.Data[0].Uncertain || detail.Data[0].State != "unknown" || len(detail.Data[0].Events) != 20 || detail.Data[0].Events[0].Sequence != 6 || detail.Data[0].ProviderReference == nil || *detail.Data[0].ProviderReference != "safe-action-42" {
		t.Fatal(detail, err)
	}
	first, err := operation.Read(ctx, pool, actor, lease.Workspace, uuid.Nil, "", 1)
	if err != nil || first.NextCursor == nil {
		t.Fatal(first, err)
	}
	second, err := operation.Read(ctx, pool, actor, lease.Workspace, uuid.Nil, *first.NextCursor, 1)
	if err != nil || len(second.Data) != 1 || first.Data[0].ID == second.Data[0].ID || first.Data[0].ID.String() <= second.Data[0].ID.String() {
		t.Fatal("cursor", err)
	}
	if _, err = pool.Exec(ctx, `UPDATE operations SET provider_reference='https://unsafe.example/token' WHERE id=$1`, other); err == nil {
		t.Fatal("unsafe native reference")
	}
	for _, state := range []string{"queued", "dispatching", "waiting_provider", "reconciling", "unknown", "succeeded", "failed", "cancelled"} {
		if _, err = pool.Exec(ctx, `UPDATE operations SET state=$2 WHERE id=$1`, id, state); err != nil {
			t.Fatal(err)
		}
		v, err := operation.Read(ctx, pool, actor, lease.Workspace, id, "", 1)
		if err != nil || v.Data[0].State != state || v.Data[0].Uncertain != (state == "unknown") {
			t.Fatal("state representation", state, err)
		}
	}
	if _, err = pool.Exec(ctx, `UPDATE connections SET status='disabled' WHERE id=$1`, lease.Connection); err != nil {
		t.Fatal(err)
	}
	if _, err = operation.Read(ctx, pool, actor, lease.Workspace, id, "", 1); err != nil {
		t.Fatal("disabled history hidden", err)
	}
	_, scope := seedIdentity(t, pool)
	if _, err = operation.Read(ctx, pool, actor, scope, id, "", 1); err != workspace.ErrNotFound {
		t.Fatal("cross scope", err)
	}
	if _, err = operation.Read(ctx, pool, actor, scope, uuid.Nil, *first.NextCursor, 1); err != workspace.ErrInvalid {
		t.Fatal("cross scope cursor", err)
	}
	if _, err = pool.Exec(ctx, `DELETE FROM connection_grants WHERE user_id=$1`, actor); err != nil {
		t.Fatal(err)
	}
	if _, err = operation.Read(ctx, pool, actor, lease.Workspace, id, "", 1); err != workspace.ErrNotFound {
		t.Fatal("revoked detail", err)
	}
	page, err := operation.Read(ctx, pool, actor, lease.Workspace, uuid.Nil, *first.NextCursor, 1)
	if err != nil || len(page.Data) != 0 {
		t.Fatal("cursor retained authority", err)
	}
}
