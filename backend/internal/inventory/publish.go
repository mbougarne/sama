package inventory

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"sama/backend/internal/job"
	"sama/backend/internal/platform"
)

// Execute scans then atomically publishes or records a safe terminal failure.
// It is an explicitly invoked handler, not a registered/running scheduler.
func (s Scanner) Execute(ctx context.Context, lease job.Lease) error {
	err := s.Scan(ctx, lease)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if finishErr := s.Complete(finishCtx, lease, err); finishErr != nil {
		return finishErr
	}
	return err
}

// Complete fences the whole publication transaction with the current lease.
// A failed scan can only change its run/job; last complete resources stay intact.
func (s Scanner) Complete(ctx context.Context, lease job.Lease, scanErr error) error {
	tx, err := platform.BeginTx(ctx, s.Pool, pgx.TxOptions{})
	if err != nil {
		return ErrScan
	}
	defer tx.Rollback(ctx)
	var locked uuid.UUID
	if tx.QueryRow(ctx, `SELECT id FROM workspaces WHERE id=$1 FOR UPDATE`, lease.Workspace).Scan(&locked) != nil {
		return ErrScan
	}
	var run, generation uuid.UUID
	var actor *uuid.UUID
	var version *int64
	var scope, kind string
	var complete bool
	var number int64
	err = tx.QueryRow(ctx, `SELECT s.id,s.actor_id,s.credential_version,s.scope,s.scan_complete,s.generation_number,s.generation,s.resource_type FROM sync_runs s
 JOIN jobs j ON (j.workspace_id,j.connection_id,j.sync_id)=(s.workspace_id,s.connection_id,s.id)
 WHERE j.id=$1 AND j.workspace_id=$2 AND j.connection_id=$3 AND j.lease_owner=$4 AND j.lease_token=$5
 AND j.state='running' AND j.lease_expires_at>clock_timestamp() AND j.deadline>clock_timestamp()
 AND s.status IN ('pending','running') FOR UPDATE OF j,s`, lease.ID, lease.Workspace, lease.Connection, lease.Owner, lease.Token).Scan(&run, &actor, &version, &scope, &complete, &number, &generation, &kind)
	if err != nil {
		return job.ErrLease
	}
	state := "succeeded"
	var code *string
	if scanErr != nil {
		state = "failed"
		safe := failureCode(scanErr)
		code = &safe
	} else {
		if !complete || actor == nil || version == nil {
			return ErrScan
		}
		binding, err := scanAuthority(ctx, tx, lease, run, *actor, *version)
		if err != nil {
			return err
		}
		var newer bool
		if tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM sync_runs WHERE workspace_id=$1 AND connection_id=$2
   AND resource_type=$5 AND scope=$3 AND status='succeeded' AND generation_number>$4)`, lease.Workspace, lease.Connection, scope, number, kind).Scan(&newer) != nil {
			return ErrScan
		}
		if newer {
			state = "cancelled"
		} else {
			_, err = tx.Exec(ctx, `INSERT INTO resources(id,workspace_id,connection_id,api_family,resource_type,scope,native_id,native_status,status,name,details_version,details,observed_at,generation,tombstoned_at,version)
    SELECT id,workspace_id,connection_id,api_family,resource_type,scope,native_id,native_status,status,name,details_version,details,observed_at,generation,NULL,1
    FROM inventory_staging WHERE workspace_id=$1 AND connection_id=$2 AND generation=$3
    ON CONFLICT(workspace_id,connection_id,api_family,resource_type,scope,native_id) DO UPDATE
    SET native_status=EXCLUDED.native_status,status=EXCLUDED.status,name=EXCLUDED.name,details=EXCLUDED.details,
    observed_at=EXCLUDED.observed_at,generation=EXCLUDED.generation,tombstoned_at=NULL,version=resources.version+1
    WHERE EXISTS(SELECT 1 FROM sync_runs old WHERE old.workspace_id=resources.workspace_id AND old.connection_id=resources.connection_id AND old.scope=resources.scope AND old.resource_type=resources.resource_type AND old.generation=resources.generation AND old.generation_number<=$4)`, lease.Workspace, lease.Connection, generation, number)
			if err != nil {
				return ErrScan
			}
			_, err = tx.Exec(ctx, `UPDATE resources r SET tombstoned_at=clock_timestamp(),version=version+1
    WHERE r.workspace_id=$1 AND r.connection_id=$2 AND r.api_family=$3 AND r.resource_type=$7
    AND r.scope=$4 AND r.tombstoned_at IS NULL
    AND EXISTS(SELECT 1 FROM sync_runs old WHERE old.workspace_id=r.workspace_id AND old.connection_id=r.connection_id AND old.scope=r.scope AND old.resource_type=r.resource_type AND old.generation=r.generation AND old.generation_number<=$6)
    AND NOT EXISTS(SELECT 1 FROM inventory_staging i WHERE i.workspace_id=r.workspace_id
      AND i.connection_id=r.connection_id AND i.generation=$5 AND i.native_id=r.native_id)`, lease.Workspace, lease.Connection, binding.Family(), scope, generation, number, kind)
			if err != nil {
				return ErrScan
			}
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE sync_runs SET status=$2,failure_code=$3,completed_at=clock_timestamp() WHERE id=$1`, run, state, code); err != nil {
		return ErrScan
	}
	if _, err = tx.Exec(ctx, `UPDATE jobs SET state=$2,completed_at=clock_timestamp(),lease_owner=NULL,lease_token=NULL,lease_expires_at=NULL WHERE id=$1`, lease.ID, state); err != nil {
		return ErrScan
	}
	if tx.Commit(ctx) != nil {
		return ErrScan
	}
	return nil
}
