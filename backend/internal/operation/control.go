package operation

import (
	"context"
	"errors"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"sama/backend/internal/audit"
	"sama/backend/internal/identity"
	"sama/backend/internal/job"
	"sama/backend/internal/platform"
	"sama/backend/internal/workspace"
)

func controlLoad(ctx context.Context, tx pgx.Tx, actor, scope, id uuid.UUID) (dispatchPlan, string, error) {
	var p dispatchPlan
	var locked uuid.UUID
	if tx.QueryRow(ctx, `SELECT id FROM workspaces WHERE id=$1 AND closed_at IS NULL FOR UPDATE`, scope).Scan(&locked) != nil {
		return p, "", workspace.ErrNotFound
	}
	var role string
	var grants []string
	var high bool
	err := tx.QueryRow(ctx, `SELECT o.id,o.actor_id,o.resource_id,o.state,o.connection_id,m.role,g.actions,r.high_impact FROM operations o
 JOIN action_reviews r ON (r.workspace_id,r.connection_id,r.id)=(o.workspace_id,o.connection_id,o.review_id)
 JOIN memberships m ON m.workspace_id=o.workspace_id AND m.user_id=$2 JOIN users u ON u.id=m.user_id AND u.disabled_at IS NULL
 JOIN connection_grants g ON (g.workspace_id,g.connection_id,g.user_id)=(o.workspace_id,o.connection_id,m.user_id)
 WHERE o.workspace_id=$1 AND o.id=$3 AND 'read'=ANY(g.actions) FOR UPDATE OF o`, scope, actor, id).Scan(&p.id, &p.actor, &p.target, &p.state, &p.review.connection, &role, &grants, &high)
	if errors.Is(err, pgx.ErrNoRows) {
		return p, "", workspace.ErrNotFound
	}
	if err != nil {
		return p, "", ErrStore
	}
	class := "operate"
	if high {
		class = "high_impact"
	}
	if !workspace.WithinCeiling(role, class) || !slices.Contains(grants, class) {
		return p, role, ErrDenied
	}
	return p, role, nil
}

func controlAudit(ctx context.Context, tx pgx.Tx, p dispatchPlan, actor, scope uuid.UUID, request, reason string) (uuid.UUID, error) {
	id, err := audit.NewID()
	if err != nil {
		return uuid.Nil, ErrStore
	}
	kind := "operation.reconciled"
	if reason == "cancelled_before_dispatch" {
		kind = "operation.cancelled"
	}
	if reason == "reservation_released_residual_risk" {
		kind = "operation.reservation_released"
	}
	err = audit.Append(ctx, tx, audit.Event{ID: id, Workspace: scope, ActorKind: audit.ActorUser, ActorID: &actor, Type: kind, RequestID: request, Metadata: map[string]string{"operation_id": p.id.String(), "resource_id": p.target.String(), "reason_code": reason}})
	if err != nil {
		return uuid.Nil, ErrStore
	}
	return id, nil
}

// Cancel competes with marker persistence under the same workspace/operation
// locks. It never invokes provider cancellation for submitted work.
func (s *Service) Cancel(ctx context.Context, p identity.Principal, scope, id uuid.UUID, request string) (Accepted, error) {
	if s == nil || s.pool == nil {
		return Accepted{}, ErrStore
	}
	tx, err := platform.BeginTx(ctx, s.pool, pgx.TxOptions{})
	if err != nil {
		return Accepted{}, ErrStore
	}
	defer tx.Rollback(ctx)
	pl, _, err := controlLoad(ctx, tx, p.UserID, scope, id)
	if err != nil {
		return Accepted{}, err
	}
	if pl.state != "queued" {
		return Accepted{}, ErrState
	}
	if err = advance(ctx, tx, job.Lease{Record: job.Record{Workspace: scope}}, pl, "cancelled"); err != nil {
		return Accepted{}, err
	}
	if _, err = tx.Exec(ctx, `UPDATE jobs SET state='cancelled',completed_at=clock_timestamp(),lease_owner=NULL,lease_token=NULL,lease_expires_at=NULL WHERE workspace_id=$1 AND operation_id=$2 AND state IN ('queued','running')`, scope, id); err != nil {
		return Accepted{}, ErrStore
	}
	if _, err = controlAudit(ctx, tx, pl, p.UserID, scope, request, "cancelled_before_dispatch"); err != nil {
		return Accepted{}, err
	}
	if tx.Commit(ctx) != nil {
		return Accepted{}, ErrStore
	}
	return Accepted{id, "cancelled"}, nil
}
