//go:build integration

package integration

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"sama/backend/internal/identity"
	"sama/backend/internal/job"
	"sama/backend/internal/operation"
	"sama/backend/internal/workspace"
)

func TestAcceptanceRechecksAuthorityAfterFreshProviderRead(t *testing.T) {
	for _, probe := range []struct {
		sql      string
		expected error
	}{
		{`UPDATE connection_grants SET actions=ARRAY['read'] WHERE user_id=$1 AND workspace_id=$2`, operation.ErrDenied},
		{`UPDATE workspaces SET policy_version=policy_version+1 WHERE id=$2 AND $1::uuid IS NOT NULL`, operation.ErrStale},
		{`UPDATE connections SET status='disabled' WHERE workspace_id=$2 AND $1::uuid IS NOT NULL`, workspace.ErrNotFound},
	} {
		t.Run(probe.sql, func(t *testing.T) {
			calls := 0
			s, pool, lease, actor, target, _, _ := acceptanceSetup(t, func(pool *pgxpool.Pool, l job.Lease, a uuid.UUID) {
				calls++
				if calls == 2 {
					if _, err := pool.Exec(context.Background(), probe.sql, a, l.Workspace); err != nil {
						t.Fatal(err)
					}
				}
			})
			c := prepareAcceptance(t, s, actor, lease.Workspace, target)
			if _, err := s.Confirm(context.Background(), identity.Principal{UserID: actor}, lease.Workspace, "authority-key", c, "authority-test"); err != probe.expected {
				t.Fatal("post-read change accepted", err)
			}
			var count int
			if pool.QueryRow(context.Background(), `SELECT count(*) FROM operations`).Scan(&count) != nil || count != 0 {
				t.Fatal("rejected authority wrote operation")
			}
		})
	}
}

func TestAcceptanceRequiresTypedHighImpactEvidenceAndPreservesConflict(t *testing.T) {
	s, pool, lease, actor, target, pre, _ := acceptanceSetup(t)
	pre.HighImpact = true
	p := identity.Principal{UserID: actor, Reauthenticated: true, AuthenticatedAt: time.Now().Add(-time.Minute)}
	review, err := s.Prepare(context.Background(), p, lease.Workspace, target, operation.Intent{Action: "compute.server.power_on", Parameters: &operation.PowerParameters{}})
	if err != nil {
		t.Fatal(err)
	}
	c := operation.Confirmation{ReviewID: review.ID, Confirmed: true, TypedTarget: "wrong"}
	if _, err = s.Confirm(context.Background(), p, lease.Workspace, "high-key", c, "high-test"); err != operation.ErrInvalid {
		t.Fatal("wrong typed target", err)
	}
	c.TypedTarget = "Safe"
	stale := p
	stale.Reauthenticated = false
	if _, err = s.Confirm(context.Background(), stale, lease.Workspace, "high-key", c, "high-test"); err != identity.ErrRecentRequired {
		t.Fatal("unverified authentication accepted", err)
	}
	accepted, err := s.Confirm(context.Background(), p, lease.Workspace, "high-key", c, "high-test")
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.Prepare(context.Background(), p, lease.Workspace, target, operation.Intent{Action: "compute.server.power_on", Parameters: &operation.PowerParameters{}})
	if err != nil {
		t.Fatal(err)
	}
	c.ReviewID = second.ID
	if _, err = s.Confirm(context.Background(), p, lease.Workspace, "high-key", c, "conflict-test"); err != operation.ErrConflict {
		t.Fatal("different review reused key", err)
	}
	var count int
	if pool.QueryRow(context.Background(), `SELECT count(*) FROM operations WHERE id=$1`, accepted.ID).Scan(&count) != nil || count != 1 {
		t.Fatal("conflict modified operation")
	}
}
