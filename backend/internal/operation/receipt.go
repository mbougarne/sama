package operation

import (
	"context"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5"
	"sama/backend/internal/audit"
	"sama/backend/internal/job"
	"sama/backend/internal/platform"
	"sama/backend/internal/provider"
)

var nativeReference = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

func classifyReceipt(r provider.Receipt, err error) (string, *string) {
	if err != nil {
		return "reconciling", nil
	}
	if r.Outcome == "rejected" && r.Reference == "" {
		return "failed", nil
	}
	if r.Outcome == "accepted" && nativeReference.MatchString(r.Reference) {
		return "waiting_provider", &r.Reference
	}
	return "reconciling", nil
}

func (d Dispatcher) receipt(ctx context.Context, lease job.Lease, r provider.Receipt, submitErr error) error {
	to, reference := classifyReceipt(r, submitErr)
	tx, err := platform.BeginTx(ctx, d.Service.pool, pgx.TxOptions{})
	if err != nil {
		return ErrStore
	}
	defer tx.Rollback(ctx)
	p, err := d.Service.dispatchLoad(ctx, tx, lease)
	if err != nil {
		return err
	}
	if p.state != "dispatching" {
		return ErrState
	}
	if _, err = tx.Exec(ctx, `UPDATE operations SET provider_reference=$3 WHERE workspace_id=$1 AND id=$2`, lease.Workspace, p.id, reference); err != nil {
		return ErrStore
	}
	if err = advance(ctx, tx, lease, p, to); err != nil {
		return err
	}
	state := "queued"
	if to == "failed" {
		state = "failed"
	}
	if err = executionAudit(ctx, tx, lease, p, to); err != nil {
		return err
	}
	next := time.Now().Add(2 * time.Second)
	if next.After(lease.Deadline) {
		next = lease.Deadline
	}
	if err = finishJob(ctx, tx, lease, state, next); err != nil {
		return err
	}
	if tx.Commit(ctx) != nil {
		return ErrStore
	}
	return nil
}

func executionAudit(ctx context.Context, tx pgx.Tx, lease job.Lease, p dispatchPlan, outcome string) error {
	id, err := audit.NewID()
	if err != nil {
		return ErrStore
	}
	kind := "operation.reconciled"
	if outcome == "failed" {
		kind = "operation.failed"
	}
	if outcome == "succeeded" {
		kind = "operation.completed"
	}
	metadata := map[string]string{"operation_id": p.id.String(), "resource_id": p.target.String()}
	if kind != "operation.completed" {
		metadata["reason_code"] = outcome
	}
	if audit.Append(ctx, tx, audit.Event{ID: id, Workspace: lease.Workspace, ActorKind: audit.ActorSystem, Type: kind, RequestID: "operation-worker", Metadata: metadata}) != nil {
		return ErrStore
	}
	return nil
}
