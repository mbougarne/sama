package workspace

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"time"
)

type Observations struct {
	LastSuccessAt *time.Time `json:"last_success_at"`
	FailureCode   *string    `json:"failure_code"`
	State         string     `json:"state"`
}
type ConnectionHealth struct {
	ID                    uuid.UUID    `json:"id"`
	Label                 string       `json:"label"`
	Provider              string       `json:"provider"`
	AccountIdentity       string       `json:"account_identity"`
	Status                string       `json:"status"`
	PermissionsVerifiedAt *time.Time   `json:"permissions_verified_at"`
	Inventory             Observations `json:"inventory"`
}
type Overview struct {
	ConnectionCount      int64              `json:"connection_count"`
	ResourceCount        int64              `json:"resource_count"`
	PendingOperations    int64              `json:"pending_operations"`
	UnknownOperations    int64              `json:"unknown_operations"`
	StaleConnections     int64              `json:"stale_connections"`
	Connections          []ConnectionHealth `json:"connections"`
	ConnectionsTruncated bool               `json:"connections_truncated"`
}

// ReadOverview is one local PostgreSQL snapshot; opening it cannot call providers
// or enqueue work. Its response caps connection health rows independently of totals.
func ReadOverview(ctx context.Context, pool *pgxpool.Pool, actor, scope uuid.UUID) (Overview, error) {
	var result Overview
	err := pool.QueryRow(ctx, `WITH authority AS (
 SELECT w.id FROM workspaces w JOIN memberships m ON m.workspace_id=w.id AND m.user_id=$2
 JOIN users u ON u.id=m.user_id AND u.disabled_at IS NULL WHERE w.id=$1 AND w.closed_at IS NULL),
 allowed AS (SELECT c.* FROM connections c JOIN authority a ON c.workspace_id=a.id
 JOIN connection_grants g ON (g.workspace_id,g.connection_id,g.user_id)=(c.workspace_id,c.id,$2) WHERE 'read'=ANY(g.actions)),
 freshness AS (SELECT c.id,c.label,c.provider,c.account_identity,c.status,c.permissions_verified_at,
 (SELECT max(completed_at) FROM sync_runs s WHERE s.workspace_id=c.workspace_id AND s.connection_id=c.id AND s.scope='global' AND s.status='succeeded') AS last_success_at,
 (SELECT failure_code FROM sync_runs s WHERE s.workspace_id=c.workspace_id AND s.connection_id=c.id AND s.scope='global' ORDER BY generation_number DESC LIMIT 1) AS failure_code FROM allowed c),
 health AS (SELECT f.*,CASE WHEN last_success_at IS NULL THEN 'never_observed'
 WHEN failure_code IS NULL AND last_success_at BETWEEN clock_timestamp()-interval '5 minutes' AND clock_timestamp() THEN 'fresh' ELSE 'stale' END AS inventory_state FROM freshness f)
 SELECT (SELECT count(*) FROM allowed),
 (SELECT count(*) FROM resources r JOIN allowed c ON c.workspace_id=r.workspace_id AND c.id=r.connection_id
 JOIN sync_runs s ON (s.workspace_id,s.connection_id,s.generation,s.resource_type,s.scope)=(r.workspace_id,r.connection_id,r.generation,r.resource_type,r.scope)
 WHERE r.tombstoned_at IS NULL AND s.status='succeeded'),
 (SELECT count(*) FROM operations o JOIN allowed c ON (c.workspace_id,c.id)=(o.workspace_id,o.connection_id) WHERE o.state IN ('queued','dispatching','waiting_provider','reconciling')),
 (SELECT count(*) FROM operations o JOIN allowed c ON (c.workspace_id,c.id)=(o.workspace_id,o.connection_id) WHERE o.state='unknown'),
 (SELECT count(*) FROM health WHERE inventory_state<>'fresh'),
 COALESCE((SELECT jsonb_agg(jsonb_build_object('id',id,'label',label,'provider',provider,'account_identity',account_identity,'status',status,'permissions_verified_at',permissions_verified_at,
 'inventory',jsonb_build_object('last_success_at',last_success_at,'failure_code',failure_code,'state',inventory_state)) ORDER BY id) FROM (SELECT * FROM health ORDER BY id LIMIT 50) bounded),'[]'::jsonb)
 FROM authority`, scope, actor).Scan(&result.ConnectionCount, &result.ResourceCount, &result.PendingOperations, &result.UnknownOperations, &result.StaleConnections, &result.Connections)
	if errors.Is(err, pgx.ErrNoRows) {
		return Overview{}, ErrNotFound
	}
	if err != nil {
		return Overview{}, ErrStore
	}
	result.ConnectionsTruncated = result.ConnectionCount > 50
	return result, nil
}
