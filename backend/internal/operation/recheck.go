package operation

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"sama/backend/internal/identity"
	"sama/backend/internal/job"
	"sama/backend/internal/platform"
)

// Recheck reuses the unique operation job with a new five-minute observation
// budget. It never returns the original intent to queued or permits submission.
func (s *Service) Recheck(ctx context.Context, p identity.Principal, scope, id uuid.UUID, request string) (Accepted, error) {
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
	if pl.state == "reconciling" {
		var pending bool
		if tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM jobs WHERE workspace_id=$1 AND operation_id=$2 AND state IN ('queued','running') AND deadline>clock_timestamp())`, scope, id).Scan(&pending) != nil {
			return Accepted{}, ErrStore
		}
		if pending {
			return Accepted{id, "reconciling"}, nil
		}
	}
	if pl.state != "unknown" {
		return Accepted{}, ErrState
	}
	var reserved bool
	if tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM resource_locks WHERE workspace_id=$1 AND operation_id=$2)`, scope, id).Scan(&reserved) != nil {
		return Accepted{}, ErrStore
	}
	if !reserved {
		return Accepted{}, ErrState
	}
	if _, err = controlAudit(ctx, tx, pl, p.UserID, scope, request, "user_requested_recheck"); err != nil {
		return Accepted{}, err
	}
	if err = advance(ctx, tx, job.Lease{Record: job.Record{Workspace: scope}}, pl, "reconciling"); err != nil {
		return Accepted{}, err
	}
	result, err := tx.Exec(ctx, `UPDATE jobs SET state='queued',deadline=clock_timestamp()+interval '5 minutes',next_run_at=clock_timestamp(),attempts=0,
 completed_at=NULL,lease_owner=NULL,lease_token=NULL,lease_expires_at=NULL WHERE workspace_id=$1 AND operation_id=$2 AND type='operation_dispatch' AND payload_version=3`, scope, id)
	if err != nil || result.RowsAffected() != 1 {
		return Accepted{}, ErrStore
	}
	if tx.Commit(ctx) != nil {
		return Accepted{}, ErrStore
	}
	return Accepted{id, "reconciling"}, nil
}
