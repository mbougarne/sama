package inventory

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	"sama/backend/internal/platform"
)

// PruneStaging removes at most 100 old unpublished/copy staging rows. It never
// deletes sync references or the public observations needed for last-good state.
func PruneStaging(ctx context.Context, pool *pgxpool.Pool) (int64, error) {
	result, err := platform.Exec(ctx, pool, `WITH expired AS (
 SELECT i.workspace_id,i.connection_id,i.generation,i.native_id FROM inventory_staging i
 JOIN sync_runs s ON (s.workspace_id,s.connection_id,s.scope,s.generation)=(i.workspace_id,i.connection_id,i.scope,i.generation)
 WHERE s.status IN ('succeeded','failed','cancelled') AND s.completed_at<clock_timestamp()-interval '7 days'
 ORDER BY s.completed_at,i.id FOR UPDATE OF i SKIP LOCKED LIMIT 100)
 DELETE FROM inventory_staging i USING expired e WHERE (i.workspace_id,i.connection_id,i.generation,i.native_id)=(e.workspace_id,e.connection_id,e.generation,e.native_id)`)
	if err != nil {
		return 0, ErrScan
	}
	return result.RowsAffected(), nil
}
