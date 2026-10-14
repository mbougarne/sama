package operation

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"sama/backend/internal/job"
	"sama/backend/internal/platform"
)

// Expire checkpoints at most 50 exhausted jobs, never while a live lease can
// write. Unsent work fails; submitted work becomes unknown and stays reserved.
func Expire(ctx context.Context, pool *pgxpool.Pool) error {
	rows, err := pool.Query(ctx, `SELECT id,workspace_id,connection_id FROM jobs WHERE type='operation_dispatch' AND state IN ('queued','running')
 AND (deadline<=clock_timestamp() OR attempts>=100) AND (lease_expires_at IS NULL OR lease_expires_at<=clock_timestamp()) ORDER BY id LIMIT 50`)
	if err != nil {
		return ErrStore
	}
	var leases []job.Lease
	for rows.Next() {
		var l job.Lease
		if rows.Scan(&l.ID, &l.Workspace, &l.Connection) != nil {
			rows.Close()
			return ErrStore
		}
		leases = append(leases, l)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return ErrStore
	}
	for _, l := range leases {
		tx, err := platform.BeginTx(ctx, pool, pgx.TxOptions{})
		if err != nil {
			return ErrStore
		}
		if _, err = tx.Exec(ctx, `SELECT id FROM workspaces WHERE id=$1 FOR UPDATE`, l.Workspace); err != nil {
			tx.Rollback(ctx)
			return ErrStore
		}
		var p dispatchPlan
		err = tx.QueryRow(ctx, `SELECT o.id,o.resource_id,o.state FROM jobs j JOIN operations o ON (o.workspace_id,o.connection_id,o.id)=(j.workspace_id,j.connection_id,j.operation_id)
 WHERE j.id=$1 AND j.state IN ('queued','running') AND (j.deadline<=clock_timestamp() OR j.attempts>=100)
 AND (j.lease_expires_at IS NULL OR j.lease_expires_at<=clock_timestamp()) FOR UPDATE OF o,j`, l.ID).Scan(&p.id, &p.target, &p.state)
		if err == pgx.ErrNoRows {
			tx.Rollback(ctx)
			continue
		}
		if err != nil {
			tx.Rollback(ctx)
			return ErrStore
		}
		to := "unknown"
		if p.state == "queued" {
			to = "failed"
		} else if p.state == "dispatching" || p.state == "waiting_provider" {
			if err = advance(ctx, tx, l, p, "reconciling"); err != nil {
				tx.Rollback(ctx)
				return err
			}
			p.state = "reconciling"
		}
		if p.state != "unknown" && p.state != "succeeded" && p.state != "failed" && p.state != "cancelled" {
			if err = advance(ctx, tx, l, p, to); err == nil {
				err = executionAudit(ctx, tx, l, p, to)
			}
		}
		if err == nil {
			_, err = tx.Exec(ctx, `UPDATE jobs SET state='failed',completed_at=clock_timestamp(),lease_owner=NULL,lease_token=NULL,lease_expires_at=NULL WHERE id=$1`, l.ID)
		}
		if err != nil || tx.Commit(ctx) != nil {
			tx.Rollback(ctx)
			return ErrStore
		}
	}
	return nil
}
