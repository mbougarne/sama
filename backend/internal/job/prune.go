package job

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	"sama/backend/internal/platform"
)

func PruneTerminal(ctx context.Context, pool *pgxpool.Pool) (int64, error) {
	result, err := platform.Exec(ctx, pool, `WITH expired AS (
 SELECT j.id FROM jobs j WHERE j.state IN ('succeeded','failed','cancelled') AND j.completed_at<clock_timestamp()-interval '7 days'
 AND (j.operation_id IS NULL OR EXISTS(SELECT 1 FROM operations o WHERE o.workspace_id=j.workspace_id AND o.id=j.operation_id AND o.state IN ('succeeded','failed','cancelled')))
 ORDER BY j.completed_at,j.id FOR UPDATE OF j SKIP LOCKED LIMIT 100)
 DELETE FROM jobs j USING expired e WHERE j.id=e.id`)
	if err != nil {
		return 0, ErrJob
	}
	return result.RowsAffected(), nil
}
