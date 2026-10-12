package operation

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"sama/backend/internal/job"
	"sama/backend/internal/platform"
)

var ErrState = errors.New("invalid operation transition")

func Allowed(from, to string) bool {
	switch from {
	case "queued":
		return to == "dispatching" || to == "cancelled" || to == "failed"
	case "dispatching":
		return to == "waiting_provider" || to == "reconciling" || to == "succeeded" || to == "failed"
	case "waiting_provider":
		return to == "waiting_provider" || to == "reconciling" || to == "succeeded" || to == "failed"
	case "reconciling":
		return to == "waiting_provider" || to == "reconciling" || to == "succeeded" || to == "failed" || to == "unknown"
	case "unknown":
		return to == "reconciling"
	default:
		return false
	}
}

// Transition atomically advances state and its event under a live operation-job
// lease. Unknown recovery additionally requires a fresh durable audit reference.
// Provider dispatch/outcome handlers still own authority and evidence checks.
func Transition(ctx context.Context, pool *pgxpool.Pool, lease job.Lease, id uuid.UUID, from, to string, recovery uuid.UUID) error {
	if !Allowed(from, to) {
		return ErrState
	}
	tx, err := platform.BeginTx(ctx, pool, pgx.TxOptions{})
	if err != nil {
		return ErrStore
	}
	defer tx.Rollback(ctx)
	var sequence int64
	err = tx.QueryRow(ctx, `UPDATE operations o SET state=$4,version=version+1,event_sequence=event_sequence+1,updated_at=clock_timestamp()
 WHERE o.workspace_id=$1 AND o.connection_id=$2 AND o.id=$3 AND o.state=$5
 AND ($5<>'unknown' OR EXISTS(SELECT 1 FROM audit_events a WHERE a.id=$9 AND a.workspace_id=o.workspace_id
  AND a.event_type='operation.reconciled' AND a.metadata->>'operation_id'=o.id::text AND a.created_at>=o.updated_at))
 AND EXISTS(SELECT 1 FROM jobs j WHERE j.id=$6 AND j.workspace_id=o.workspace_id AND j.connection_id=o.connection_id AND j.operation_id=o.id
  AND j.type='operation_dispatch' AND j.payload_version=3 AND j.state='running' AND j.lease_owner=$7 AND j.lease_token=$8
  AND j.lease_expires_at>clock_timestamp() AND j.deadline>clock_timestamp())
 RETURNING event_sequence`, lease.Workspace, lease.Connection, id, to, from, lease.ID, lease.Owner, lease.Token, recovery).Scan(&sequence)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrState
	}
	if err != nil {
		return ErrStore
	}
	if AppendEvent(ctx, tx, lease.Workspace, id, sequence, from, to) != nil {
		return ErrStore
	}
	if to == "succeeded" || to == "failed" || to == "cancelled" {
		if ReleaseTerminal(ctx, tx, lease.Workspace, id) != nil {
			return ErrStore
		}
	}
	if tx.Commit(ctx) != nil {
		return ErrStore
	}
	return nil
}
