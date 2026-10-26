package operation

import (
	"context"
	"crypto/subtle"
	"time"

	"github.com/jackc/pgx/v5"
	"sama/backend/internal/job"
	"sama/backend/internal/platform"
	"sama/backend/internal/provider"
)

type MutationFactory func(context.Context, provider.Binding) (provider.PowerMutation, error)
type Dispatcher struct {
	Service   *Service
	Factories map[string]MutationFactory
}

// Work can submit only a newly committed queued->dispatching marker. A successor
// with that marker always observes, including when the first call never left.
func (d Dispatcher) Work(ctx context.Context, lease job.Lease) error {
	s := d.Service
	if s == nil || s.pool == nil || !s.enabled {
		return ErrUnsupported
	}
	tx, err := platform.BeginTx(ctx, s.pool, pgx.TxOptions{})
	if err != nil {
		return ErrStore
	}
	p, err := s.dispatchLoad(ctx, tx, lease)
	tx.Rollback(ctx)
	if err != nil {
		return err
	}
	if p.state != "queued" {
		return d.observe(ctx, lease, p)
	}
	p, pre, err := s.inspectDispatch(ctx, lease)
	if err != nil {
		return d.rejectUnsent(ctx, lease, err)
	}
	factory := d.Factories[factoryKey(p.authority.provider+"/"+p.authority.binding.Family(), p.review.intent.Action)]
	if factory == nil {
		return d.rejectUnsent(ctx, lease, ErrUnsupported)
	}
	adapter, err := factory(ctx, p.authority.binding)
	if err != nil || adapter == nil {
		return ErrProvider
	}
	tx, err = platform.BeginTx(ctx, s.pool, pgx.TxOptions{})
	if err != nil {
		return ErrStore
	}
	defer tx.Rollback(ctx)
	current, err := s.dispatchLoad(ctx, tx, lease)
	if err == nil {
		err = s.dispatchCheck(ctx, tx, lease, &current)
	}
	if err == nil {
		err = checkRecoveryGate(ctx, tx)
	}
	if err != nil {
		return err
	}
	if subtle.ConstantTimeCompare(pre.Fingerprint[:], current.review.preconditions) != 1 {
		return ErrStale
	}
	if err = advance(ctx, tx, lease, current, "dispatching"); err != nil {
		return err
	}
	if tx.Commit(ctx) != nil {
		return ErrStore
	}
	// A DB fencing token cannot fence this external call. Cancellation narrows
	// the window; any crash after the marker is conservatively ambiguous.
	if ctx.Err() != nil {
		return ctx.Err()
	}
	call, stop := context.WithTimeout(ctx, 30*time.Second)
	var receipt provider.Receipt
	submitErr := s.budget.Call(call, current.authority.binding.Connection(), quotaKey(current.authority.provider, current.authority.binding.Family(), current.authority.account), func(call context.Context) error {
		var e error
		receipt, e = adapter.Submit(call, current.authority.binding, current.review.intent.Action, current.authority.native, pre.Fingerprint)
		return e
	})
	stop()
	return d.receipt(ctx, lease, receipt, submitErr)
}

func (d Dispatcher) rejectUnsent(ctx context.Context, lease job.Lease, cause error) error {
	// Temporary read/store failures leave queued work recoverable. A revoked
	// authority or changed review is definitive evidence that no call is allowed.
	if cause != ErrDenied && cause != ErrStale && cause != ErrUnsupported {
		return cause
	}
	tx, err := platform.BeginTx(ctx, d.Service.pool, pgx.TxOptions{})
	if err != nil {
		return ErrStore
	}
	defer tx.Rollback(ctx)
	p, err := d.Service.dispatchLoad(ctx, tx, lease)
	if err != nil {
		return err
	}
	if p.state != "queued" {
		return ErrState
	}
	if err = advance(ctx, tx, lease, p, "failed"); err != nil {
		return err
	}
	if err = executionAudit(ctx, tx, lease, p, "failed"); err != nil {
		return err
	}
	if err = finishJob(ctx, tx, lease, "failed", time.Time{}); err != nil {
		return err
	}
	if tx.Commit(ctx) != nil {
		return ErrStore
	}
	return nil
}

func advance(ctx context.Context, tx pgx.Tx, lease job.Lease, p dispatchPlan, to string) error {
	if !Allowed(p.state, to) {
		return ErrState
	}
	var sequence int64
	if tx.QueryRow(ctx, `UPDATE operations SET state=$3,version=version+1,event_sequence=event_sequence+1,updated_at=clock_timestamp()
 WHERE workspace_id=$1 AND id=$2 AND state=$4 RETURNING event_sequence`, lease.Workspace, p.id, to, p.state).Scan(&sequence) != nil {
		return ErrState
	}
	if AppendEvent(ctx, tx, lease.Workspace, p.id, sequence, p.state, to) != nil {
		return ErrStore
	}
	if to == "succeeded" || to == "failed" || to == "cancelled" {
		return ReleaseTerminal(ctx, tx, lease.Workspace, p.id)
	}
	return nil
}

func finishJob(ctx context.Context, tx pgx.Tx, lease job.Lease, state string, next time.Time) error {
	result, err := tx.Exec(ctx, `UPDATE jobs SET state=$4,next_run_at=CASE WHEN $4='queued' THEN $5 ELSE next_run_at END,
 completed_at=CASE WHEN $4='queued' THEN NULL ELSE clock_timestamp() END,lease_owner=NULL,lease_token=NULL,lease_expires_at=NULL
 WHERE id=$1 AND lease_owner=$2 AND lease_token=$3 AND state='running' AND lease_expires_at>clock_timestamp() AND deadline>clock_timestamp()`, lease.ID, lease.Owner, lease.Token, state, next)
	if err != nil || result.RowsAffected() != 1 {
		return job.ErrLease
	}
	return nil
}

// Finish preserves the marker on any failure, including receipt persistence or
// lease loss. Recovery cannot re-enter submission from dispatching.
func (d Dispatcher) Finish(context.Context, job.Lease, error) error { return nil }
