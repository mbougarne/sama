package operation

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var ErrBusy = errors.New("resource reserved by another operation")

type Target struct {
	Kind     string
	Scope    string
	NativeID string
}

// Reserve accepts one target only and joins acceptance's transaction. Creation
// slots are storage primitives; no creation action is enabled by this helper.
func Reserve(ctx context.Context, tx pgx.Tx, workspace, connection, id uuid.UUID, targets []Target) (int64, error) {
	if len(targets) != 1 {
		return 0, ErrInvalid
	}
	target := targets[0]
	if target.Kind != "resource" && target.Kind != "creation" || len(target.Scope) == 0 || len(target.Scope) > 128 || len(target.NativeID) == 0 || len(target.NativeID) > 512 {
		return 0, ErrInvalid
	}
	var state, scope, native string
	if tx.QueryRow(ctx, `SELECT o.state,r.scope,r.native_id FROM operations o JOIN resources r
 ON (r.workspace_id,r.connection_id,r.id)=(o.workspace_id,o.connection_id,o.resource_id)
 WHERE o.workspace_id=$1 AND o.connection_id=$2 AND o.id=$3 FOR UPDATE OF o`, workspace, connection, id).Scan(&state, &scope, &native) != nil {
		return 0, ErrStore
	}
	if state != "queued" || target.Kind == "resource" && (scope != target.Scope || native != target.NativeID) {
		return 0, ErrInvalid
	}
	var fence int64
	err := tx.QueryRow(ctx, `INSERT INTO resource_locks(workspace_id,connection_id,target_kind,resource_type,scope,native_id,operation_id)
 VALUES($1,$2,$3,'compute.server',$4,$5,$6) ON CONFLICT DO NOTHING RETURNING fencing_version`, workspace, connection, target.Kind, target.Scope, target.NativeID, id).Scan(&fence)
	if errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(ctx, `SELECT fencing_version FROM resource_locks WHERE workspace_id=$1 AND connection_id=$2
   AND target_kind=$3 AND scope=$4 AND native_id=$5 AND operation_id=$6`, workspace, connection, target.Kind, target.Scope, target.NativeID, id).Scan(&fence)
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, ErrBusy
		}
	}
	if err != nil {
		return 0, ErrStore
	}
	return fence, nil
}

// ReleaseTerminal cannot release unknown or in-flight outcomes, including when
// a worker lease has expired. Caller state/event and release share one commit.
func ReleaseTerminal(ctx context.Context, tx pgx.Tx, workspace, id uuid.UUID) error {
	var state string
	if tx.QueryRow(ctx, `SELECT state FROM operations WHERE workspace_id=$1 AND id=$2 FOR UPDATE`, workspace, id).Scan(&state) != nil {
		return ErrStore
	}
	if state != "succeeded" && state != "failed" && state != "cancelled" {
		return ErrState
	}
	if _, err := tx.Exec(ctx, `DELETE FROM resource_locks WHERE workspace_id=$1 AND operation_id=$2`, workspace, id); err != nil {
		return ErrStore
	}
	return nil
}
