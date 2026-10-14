package operation

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"sama/backend/internal/platform"
)

var ErrRecovery = errors.New("installation dispatch recovery gate closed")

func checkRecoveryGate(ctx context.Context, tx pgx.Tx) error {
	var open bool
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock_shared(1396789850)`); err != nil {
		return ErrStore
	}
	if tx.QueryRow(ctx, `SELECT open FROM dispatch_recovery_gate WHERE singleton=true`).Scan(&open) != nil {
		return ErrStore
	}
	if !open {
		return ErrRecovery
	}
	return nil
}

type RecoveryStatus struct {
	Open       bool
	Version    int64
	Unresolved int
}

func Recovery(ctx context.Context, pool *pgxpool.Pool) (RecoveryStatus, error) {
	var status RecoveryStatus
	err := pool.QueryRow(ctx, `SELECT open,version,(SELECT count(*) FROM operations WHERE (state IN ('dispatching','waiting_provider','reconciling') OR state='unknown' AND EXISTS(SELECT 1 FROM resource_locks r WHERE r.operation_id=operations.id))) FROM dispatch_recovery_gate WHERE singleton`).Scan(&status.Open, &status.Version, &status.Unresolved)
	if err != nil {
		return status, ErrStore
	}
	return status, nil
}

// SetRecovery is installation-only, called through the migration identity.
// Reopening requires the exact inspected version and no unresolved submissions.
func SetRecovery(ctx context.Context, pool *pgxpool.Pool, open bool, version int64) error {
	tx, err := platform.BeginTx(ctx, pool, pgx.TxOptions{})
	if err != nil {
		return ErrStore
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(1396789850)`); err != nil {
		return ErrStore
	}
	var current int64
	if tx.QueryRow(ctx, `SELECT version FROM dispatch_recovery_gate WHERE singleton FOR UPDATE`).Scan(&current) != nil {
		return ErrStore
	}
	if open {
		var count int
		if version != current {
			return ErrStale
		}
		if tx.QueryRow(ctx, `SELECT count(*) FROM operations WHERE (state IN ('dispatching','waiting_provider','reconciling') OR state='unknown' AND EXISTS(SELECT 1 FROM resource_locks r WHERE r.operation_id=operations.id))`).Scan(&count) != nil {
			return ErrStore
		}
		if count != 0 {
			return ErrRecovery
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE dispatch_recovery_gate SET open=$1,version=version+1,changed_at=clock_timestamp() WHERE singleton`, open); err != nil {
		return ErrStore
	}
	if tx.Commit(ctx) != nil {
		return ErrStore
	}
	return nil
}
