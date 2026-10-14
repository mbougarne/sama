package operation

import (
	"context"
	"crypto/rand"
	"math/big"
	"time"

	"github.com/jackc/pgx/v5"
	"sama/backend/internal/inventory"
	"sama/backend/internal/job"
	"sama/backend/internal/platform"
	"sama/backend/internal/provider"
	"sama/backend/internal/workspace"
)

func (d Dispatcher) observe(ctx context.Context, lease job.Lease, p dispatchPlan) error {
	if p.state != "dispatching" && p.state != "waiting_provider" && p.state != "reconciling" {
		return ErrState
	}
	// Submitted work is observed even after actor revocation/action disable.
	// Rotation must have verified the same account; never fall back to ambient
	// credentials or another connection. Disabled connections retain history.
	var family, brand, native, account string
	var version int64
	err := d.Service.pool.QueryRow(ctx, `SELECT c.provider,c.api_family,c.active_credential_version,r.native_id,c.account_identity FROM connections c
 JOIN resources r ON (r.workspace_id,r.connection_id,r.id)=(c.workspace_id,c.id,$3)
 JOIN credential_versions v ON (v.workspace_id,v.connection_id,v.version)=(c.workspace_id,c.id,c.active_credential_version)
 WHERE c.workspace_id=$1 AND c.id=$2 AND v.revoked_at IS NULL`, lease.Workspace, lease.Connection, p.target).Scan(&brand, &family, &version, &native, &account)
	if err != nil {
		return ErrProvider
	}
	binding, err := provider.NewBinding(lease.Workspace, lease.Connection, family, version)
	if err != nil {
		return ErrStore
	}
	factory := d.Factories[brand+"/"+family]
	if factory == nil {
		return ErrUnsupported
	}
	adapter, err := factory(ctx, binding)
	if err != nil || adapter == nil {
		return ErrProvider
	}
	call, stop := context.WithTimeout(ctx, 30*time.Second)
	var observation provider.ActionObservation
	err = d.Service.budget.Call(call, binding.Connection(), quotaKey(brand, family, account), func(call context.Context) error {
		var e error
		observation, e = adapter.Observe(call, binding, p.review.intent.Action, native, p.reference)
		return e
	})
	stop()
	to := "reconciling"
	if err == nil {
		switch observation.Outcome {
		case "pending":
			if p.reference != nil {
				to = "waiting_provider"
			}
		case "succeeded", "failed":
			if observation.NativeEvidence {
				to = observation.Outcome
			}
		case "unknown":
			to = "unknown"
		}
	}
	tx, storeErr := platform.BeginTx(ctx, d.Service.pool, pgx.TxOptions{})
	if storeErr != nil {
		return ErrStore
	}
	defer tx.Rollback(ctx)
	current, storeErr := d.Service.dispatchLoad(ctx, tx, lease)
	if storeErr != nil {
		return storeErr
	}
	if current.state != p.state {
		return ErrState
	}
	if (p.state == "dispatching" || p.state == "waiting_provider") && to == "unknown" {
		if storeErr = advance(ctx, tx, lease, current, "reconciling"); storeErr != nil {
			return storeErr
		}
		current.state = "reconciling"
	}
	if storeErr = advance(ctx, tx, lease, current, to); storeErr != nil {
		return storeErr
	}
	state := "queued"
	if to == "succeeded" || to == "failed" || to == "unknown" {
		state = "succeeded"
	}
	if storeErr = executionAudit(ctx, tx, lease, p, to); storeErr != nil {
		return storeErr
	}
	// Refresh is separately authorized; if current authority no longer permits
	// refresh, retain the last observation and its honest freshness timestamp.
	if to == "succeeded" {
		if _, storeErr = inventory.Enqueue(ctx, tx, p.actor, lease.Workspace, lease.Connection); storeErr != nil && storeErr != inventory.ErrQuota && storeErr != workspace.ErrDenied && storeErr != workspace.ErrNotFound {
			return ErrStore
		}
	}
	next := time.Now().Add(pollDelay())
	if next.After(lease.Deadline) {
		next = lease.Deadline
	}
	if storeErr = finishJob(ctx, tx, lease, state, next); storeErr != nil {
		return storeErr
	}
	if tx.Commit(ctx) != nil {
		return ErrStore
	}
	return nil
}

func pollDelay() time.Duration {
	n, err := rand.Int(rand.Reader, big.NewInt(int64(3*time.Second)))
	if err != nil {
		return 5 * time.Second
	}
	return 2*time.Second + time.Duration(n.Int64())
}
