package job

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"sama/backend/internal/platform"
)

var ErrNoJob = errors.New("no due job")
var ErrLease = errors.New("job lease unavailable")

type Lease struct {
	Record
	Token uuid.UUID
	Owner uuid.UUID
}

type Scheduler struct {
	Pool *pgxpool.Pool
	Now  func() time.Time
}

func (s Scheduler) Claim(ctx context.Context, owner uuid.UUID) (Lease, error) {
	if owner == uuid.Nil {
		return Lease{}, ErrLease
	}
	now := s.Now()
	token, err := uuid.NewRandom()
	if err != nil {
		return Lease{}, ErrLease
	}
	tx, err := platform.BeginTx(ctx, s.Pool, pgx.TxOptions{})
	if err != nil {
		return Lease{}, ErrLease
	}
	defer tx.Rollback(ctx)
	var lease Lease
	err = tx.QueryRow(ctx, `WITH candidate AS (
        SELECT id FROM jobs WHERE type='connection_refresh' AND deadline>$1 AND attempts<100
        AND ((state='queued' AND next_run_at<=$1) OR (state='running' AND lease_expires_at<=$1))
        ORDER BY next_run_at,id FOR UPDATE SKIP LOCKED LIMIT 1)
        UPDATE jobs j SET state='running',lease_owner=$2,lease_token=$3,
        lease_expires_at=LEAST($1+interval '60 seconds',deadline),attempts=attempts+1
        FROM candidate WHERE j.id=candidate.id
        RETURNING j.id,j.workspace_id,j.connection_id,j.type,j.payload_version,j.deadline`, now, owner, token).Scan(&lease.ID, &lease.Workspace, &lease.Connection, &lease.Type, &lease.PayloadVersion, &lease.Deadline)
	if errors.Is(err, pgx.ErrNoRows) {
		return Lease{}, ErrNoJob
	}
	if err != nil || tx.Commit(ctx) != nil {
		return Lease{}, ErrLease
	}
	lease.Owner = owner
	lease.Token = token
	return lease, nil
}

func (s Scheduler) Renew(ctx context.Context, lease Lease) error {
	_, err := s.renew(ctx, lease)
	return err
}

func (s Scheduler) renew(ctx context.Context, lease Lease) (time.Time, error) {
	started, now := time.Now(), s.Now()
	var expires time.Time
	err := s.Pool.QueryRow(ctx, `UPDATE jobs SET lease_expires_at=LEAST($4+interval '60 seconds',deadline)
        WHERE id=$1 AND lease_owner=$2 AND lease_token=$3 AND state='running' AND lease_expires_at>$4 AND deadline>$4
        RETURNING lease_expires_at`, lease.ID, lease.Owner, lease.Token, now).Scan(&expires)
	if err != nil {
		return time.Time{}, ErrLease
	}
	// Anchor before the query: database latency must consume the granted lease.
	return started.Add(expires.Sub(now)), nil
}

func (s Scheduler) Finish(ctx context.Context, lease Lease, state string) error {
	if state != "succeeded" && state != "failed" {
		return ErrLease
	}
	result, err := s.Pool.Exec(ctx, `UPDATE jobs SET state=$4,lease_owner=NULL,lease_token=NULL,lease_expires_at=NULL
        WHERE id=$1 AND lease_owner=$2 AND lease_token=$3 AND state='running' AND lease_expires_at>$5 AND deadline>$5`, lease.ID, lease.Owner, lease.Token, state, s.Now())
	if err != nil || result.RowsAffected() != 1 {
		return ErrLease
	}
	return nil
}

// Run validates ownership before work, then renews every 15 seconds.
// Work must honor its context; this lease fences database writes, not a provider.
func (s Scheduler) Run(ctx context.Context, lease Lease, work func(context.Context) error) error {
	deadline := time.Now().Add(lease.Deadline.Sub(s.Now()))
	if !deadline.After(time.Now()) {
		return ErrLease
	}
	ctx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	return runRenewing(ctx, ticker.C, func(ctx context.Context) (time.Time, error) { return s.renew(ctx, lease) }, work)
}

func runRenewing(ctx context.Context, ticks <-chan time.Time, renew func(context.Context) (time.Time, error), work func(context.Context) error) error {
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	initial, stop := context.WithTimeout(ctx, 60*time.Second)
	expires, err := renew(initial)
	stop()
	if err != nil || ctx.Err() != nil || !expires.After(time.Now()) {
		return ErrLease
	}
	// This watchdog stays armed while a renewal waits for PostgreSQL. A new
	// expiry is installed only after renewal succeeds before the prior expiry.
	watchdog := time.AfterFunc(time.Until(expires), func() { cancel(ErrLease) })
	defer watchdog.Stop()
	done := make(chan error, 1)
	go func() { done <- work(ctx) }()
	for {
		select {
		case err := <-done:
			if ctx.Err() != nil {
				return context.Cause(ctx)
			}
			return err
		case <-ctx.Done():
			<-done
			return context.Cause(ctx)
		case <-ticks:
			previous := expires
			expires, err = renew(ctx)
			if err != nil || ctx.Err() != nil || !previous.After(time.Now()) || !expires.After(time.Now()) {
				cancel(ErrLease)
				<-done
				return context.Cause(ctx)
			}
			watchdog.Reset(time.Until(expires))
		}
	}
}
