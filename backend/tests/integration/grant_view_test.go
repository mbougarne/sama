//go:build integration

package integration

import (
	"context"
	"sama/backend/internal/workspace"
	"testing"
)

func TestGrantViewUsesCurrentMemberAndDelegationCeilings(t *testing.T) {
	pool, lease, actor, _ := inventoryLease(t)
	ctx := context.Background()
	view, err := workspace.ReadGrantView(ctx, pool, actor, lease.Workspace, lease.Connection, actor)
	if err != nil || !view.Editable || len(view.AllowedActions) != 4 {
		t.Fatal(view, err)
	}
	if _, err = pool.Exec(ctx, `UPDATE memberships SET role='admin' WHERE user_id=$1`, actor); err != nil {
		t.Fatal(err)
	}
	view, err = workspace.ReadGrantView(ctx, pool, actor, lease.Workspace, lease.Connection, actor)
	if err != nil || view.Editable || len(view.AllowedActions) != 0 {
		t.Fatal("admin self escalation", view, err)
	}
	if _, err = pool.Exec(ctx, `UPDATE memberships SET role='viewer' WHERE user_id=$1`, actor); err != nil {
		t.Fatal(err)
	}
	if _, err = workspace.ReadGrantView(ctx, pool, actor, lease.Workspace, lease.Connection, actor); err != workspace.ErrNotFound {
		t.Fatal("stale manager", err)
	}
	_, other := seedIdentity(t, pool)
	if _, err = workspace.ReadGrantView(ctx, pool, actor, other, lease.Connection, actor); err != workspace.ErrNotFound {
		t.Fatal("cross workspace", err)
	}
}
