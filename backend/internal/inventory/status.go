package inventory

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"sama/backend/internal/workspace"
)

type Freshness struct {
	LastSuccessAt *time.Time `json:"last_success_at"`
	FailureCode   *string    `json:"failure_code"`
	State         string     `json:"state"`
}

func NewFreshness(last *time.Time, failure *string) Freshness {
	state := "never_observed"
	if last != nil {
		state = "stale"
		age := time.Since(*last)
		if failure == nil && age >= 0 && age <= 5*time.Minute {
			state = "fresh"
		}
	}
	return Freshness{last, failure, state}
}

type SyncStatus struct {
	ID           uuid.UUID  `json:"id"`
	ConnectionID uuid.UUID  `json:"connection_id"`
	Status       string     `json:"status"`
	PageCount    int        `json:"page_count"`
	StartedAt    *time.Time `json:"started_at"`
	CompletedAt  *time.Time `json:"completed_at"`
	FailureCode  *string    `json:"failure_code"`
	Inventory    Freshness  `json:"inventory"`
}

// Status reads PostgreSQL only and filters tenant/grant authority in its query.
func Status(ctx context.Context, pool *pgxpool.Pool, actor, scope, id uuid.UUID) (SyncStatus, error) {
	var result SyncStatus
	var last *time.Time
	var failure *string
	err := pool.QueryRow(ctx, `SELECT s.id,s.connection_id,s.status,s.page_count,s.started_at,s.completed_at,s.failure_code,
 (SELECT max(completed_at) FROM sync_runs WHERE workspace_id=s.workspace_id AND connection_id=s.connection_id AND scope=s.scope AND status='succeeded'),
 (SELECT failure_code FROM sync_runs WHERE workspace_id=s.workspace_id AND connection_id=s.connection_id AND scope=s.scope ORDER BY generation_number DESC LIMIT 1)
 FROM sync_runs s JOIN workspaces w ON w.id=s.workspace_id AND w.closed_at IS NULL
 JOIN memberships m ON m.workspace_id=w.id AND m.user_id=$2 JOIN users u ON u.id=m.user_id AND u.disabled_at IS NULL
 JOIN connection_grants g ON (g.workspace_id,g.connection_id,g.user_id)=(s.workspace_id,s.connection_id,m.user_id)
 WHERE s.workspace_id=$1 AND s.id=$3 AND 'read'=ANY(g.actions)`, scope, actor, id).Scan(&result.ID, &result.ConnectionID, &result.Status, &result.PageCount, &result.StartedAt, &result.CompletedAt, &result.FailureCode, &last, &failure)
	if errors.Is(err, pgx.ErrNoRows) {
		return SyncStatus{}, workspace.ErrNotFound
	}
	if err != nil {
		return SyncStatus{}, workspace.ErrStore
	}
	result.Inventory = NewFreshness(last, failure)
	return result, nil
}
