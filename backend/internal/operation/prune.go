package operation

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	"sama/backend/internal/platform"
)

func PruneReviews(ctx context.Context, pool *pgxpool.Pool) (int64, error) {
	result, err := platform.Exec(ctx, pool, `WITH expired AS (
 SELECT r.id FROM action_reviews r WHERE r.consumed_by IS NULL AND r.expires_at<clock_timestamp()-interval '7 days'
 AND NOT EXISTS(SELECT 1 FROM operations o WHERE o.workspace_id=r.workspace_id AND o.review_id=r.id)
 ORDER BY r.expires_at,r.id FOR UPDATE SKIP LOCKED LIMIT 100)
 DELETE FROM action_reviews r USING expired e WHERE r.id=e.id`)
	if err != nil {
		return 0, ErrStore
	}
	return result.RowsAffected(), nil
}
