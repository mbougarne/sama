package operation

import (
	"context"
	"crypto/subtle"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"sama/backend/internal/job"
	"sama/backend/internal/platform"
	"sama/backend/internal/provider"
)

type dispatchPlan struct {
	id, actor, target uuid.UUID
	review            stored
	authority         authority
	state             string
	reference         *string
}

func (s *Service) dispatchLoad(ctx context.Context, tx pgx.Tx, lease job.Lease) (dispatchPlan, error) {
	var p dispatchPlan
	var workspace uuid.UUID
	if tx.QueryRow(ctx, `SELECT id FROM workspaces WHERE id=$1 AND closed_at IS NULL FOR UPDATE`, lease.Workspace).Scan(&workspace) != nil {
		return p, ErrDenied
	}
	var review uuid.UUID
	err := tx.QueryRow(ctx, `SELECT o.id,o.actor_id,o.resource_id,o.review_id,o.state,o.provider_reference FROM operations o
 JOIN jobs j ON (j.workspace_id,j.connection_id,j.operation_id)=(o.workspace_id,o.connection_id,o.id)
 WHERE j.id=$1 AND j.workspace_id=$2 AND j.connection_id=$3 AND j.type='operation_dispatch' AND j.payload_version=3
 AND j.state='running' AND j.lease_owner=$4 AND j.lease_token=$5 AND j.lease_expires_at>clock_timestamp() AND j.deadline>clock_timestamp()
 FOR UPDATE OF o,j`, lease.ID, lease.Workspace, lease.Connection, lease.Owner, lease.Token).Scan(&p.id, &p.actor, &p.target, &review, &p.state, &p.reference)
	if errors.Is(err, pgx.ErrNoRows) {
		return p, job.ErrLease
	}
	if err != nil {
		return p, ErrStore
	}
	p.review, err = s.load(ctx, tx, p.actor, lease.Workspace, review)
	return p, err
}

func (s *Service) dispatchCheck(ctx context.Context, tx pgx.Tx, lease job.Lease, p *dispatchPlan) error {
	a, err := s.authority(ctx, tx, p.actor, lease.Workspace, p.target)
	if err != nil {
		return ErrDenied
	}
	if p.state != "queued" || a.binding.Connection() != lease.Connection || a.binding.Version() != p.review.credential || a.policy != p.review.policy {
		return ErrStale
	}
	if !authorized(a, p.review.intent.Action, p.review.high) {
		return ErrDenied
	}
	if s.policy != nil && !s.policy.permits(a.provider+"/"+a.binding.Family(), p.review.intent.Action) {
		return ErrUnsupported
	}
	p.authority = a
	return nil
}

// inspectDispatch performs fresh reads outside the transaction. The final
// marker transaction repeats local authorization. Local checks cannot fence a
// provider-side change when that provider offers no compare-and-swap.
func (s *Service) inspectDispatch(ctx context.Context, lease job.Lease) (dispatchPlan, provider.Preconditions, error) {
	var pre provider.Preconditions
	tx, err := platform.BeginTx(ctx, s.pool, pgx.TxOptions{})
	if err != nil {
		return dispatchPlan{}, pre, ErrStore
	}
	defer tx.Rollback(ctx)
	p, err := s.dispatchLoad(ctx, tx, lease)
	if err == nil {
		err = s.dispatchCheck(ctx, tx, lease, &p)
	}
	tx.Rollback(ctx)
	if err != nil {
		return p, pre, err
	}
	pre, err = s.inspect(ctx, p.authority, p.review.intent)
	if err != nil {
		return p, pre, err
	}
	if subtle.ConstantTimeCompare(pre.Fingerprint[:], p.review.preconditions) != 1 || pre.HighImpact != p.review.high || pre.TargetName != p.review.name {
		return p, pre, ErrStale
	}
	return p, pre, nil
}
