package inventory

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"sama/backend/internal/workspace"
)

type Filters struct {
	Connection uuid.UUID
	Provider   string
	Type       string
	Region     string
	Account    string
	SourceID   string
	SourceType string
}

type Resource struct {
	ID               uuid.UUID `json:"id"`
	ConnectionID     uuid.UUID `json:"connection_id"`
	Provider         string    `json:"provider"`
	APIFamily        string    `json:"api_family"`
	AccountIdentity  string    `json:"account_identity"`
	Type             string    `json:"type"`
	Scope            string    `json:"scope"`
	NativeID         string    `json:"native_id"`
	Name             string    `json:"name"`
	Region           string    `json:"region"`
	Status           string    `json:"status"`
	NativeStatus     string    `json:"native_status"`
	ObservedAt       time.Time `json:"observed_at"`
	Inventory        Freshness `json:"inventory"`
	details          Details
	role             string
	grants, verified []string
	connectionStatus string
}

type ResourcePage struct {
	Data       []Resource `json:"data"`
	NextCursor *string    `json:"next_cursor"`
}

type resourceCursor struct {
	Workspace uuid.UUID
	Actor     uuid.UUID
	Filter    string
	ID        uuid.UUID
}

func filterDigest(f Filters) string {
	data, _ := json.Marshal(f)
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

// List reads complete local observations. Cursors select position; every page
// rechecks authority in PostgreSQL, so editing a cursor cannot confer access.
func List(ctx context.Context, pool *pgxpool.Pool, actor, scope uuid.UUID, f Filters, cursor string, limit int) (ResourcePage, error) {
	return query(ctx, pool, actor, scope, f, cursor, limit, uuid.Nil)
}

func query(ctx context.Context, pool *pgxpool.Pool, actor, scope uuid.UUID, f Filters, cursor string, limit int, target uuid.UUID) (ResourcePage, error) {
	if limit == 0 {
		limit = 50
	}
	if f.Type == "" && target == uuid.Nil {
		f.Type = "compute.server"
	}
	if limit < 1 || limit > 200 || f.Type != "" && f.Type != "compute.server" && f.Type != SnapshotType || len(f.SourceID) > 512 || f.SourceType != "" && f.SourceType != "compute.server" && f.SourceType != "storage.volume" || len(f.Provider) > 64 || len(f.Region) > 128 || len(f.Account) > 512 || f.Connection != uuid.Nil && f.Connection.Version() != 7 {
		return ResourcePage{}, workspace.ErrInvalid
	}
	after := uuid.Nil
	digest := filterDigest(f)
	if cursor != "" {
		if len(cursor) > 1024 {
			return ResourcePage{}, workspace.ErrInvalid
		}
		data, err := base64.RawURLEncoding.DecodeString(cursor)
		if err != nil {
			return ResourcePage{}, workspace.ErrInvalid
		}
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields()
		var value resourceCursor
		if decoder.Decode(&value) != nil || decoder.Decode(new(any)) != io.EOF || value.Workspace != scope || value.Actor != actor || value.Filter != digest || value.ID.Version() != 7 {
			return ResourcePage{}, workspace.ErrInvalid
		}
		after = value.ID
	}
	rows, err := pool.Query(ctx, `SELECT r.id,r.connection_id,c.provider,r.api_family,c.account_identity,r.resource_type,r.scope,r.native_id,r.name,
 COALESCE(r.details->>'region',''),r.status,r.native_status,r.observed_at,
 (SELECT max(completed_at) FROM sync_runs WHERE workspace_id=r.workspace_id AND connection_id=r.connection_id AND scope=r.scope AND resource_type=r.resource_type AND status='succeeded'),
 (SELECT failure_code FROM sync_runs WHERE workspace_id=r.workspace_id AND connection_id=r.connection_id AND scope=r.scope AND resource_type=r.resource_type ORDER BY generation_number DESC LIMIT 1),
 r.details,m.role,g.actions,c.status,CASE WHEN c.permissions_verified_at IS NULL THEN '{}'::text[] ELSE c.verified_capabilities END
 FROM resources r JOIN sync_runs published ON (published.workspace_id,published.connection_id,published.generation)=(r.workspace_id,r.connection_id,r.generation) AND published.resource_type=r.resource_type AND published.scope=r.scope AND published.status='succeeded'
 JOIN connections c ON (c.workspace_id,c.id)=(r.workspace_id,r.connection_id)
 JOIN workspaces w ON w.id=r.workspace_id AND w.closed_at IS NULL
 JOIN memberships m ON m.workspace_id=w.id AND m.user_id=$2 JOIN users u ON u.id=m.user_id AND u.disabled_at IS NULL
 JOIN connection_grants g ON (g.workspace_id,g.connection_id,g.user_id)=(r.workspace_id,r.connection_id,m.user_id)
 WHERE r.workspace_id=$1 AND 'read'=ANY(g.actions) AND r.tombstoned_at IS NULL AND r.id>$3
 AND ($4='' OR r.resource_type=$4) AND ($5::uuid='00000000-0000-0000-0000-000000000000' OR r.connection_id=$5)
 AND ($6='' OR r.details->>'region'=$6) AND ($7='' OR c.account_identity=$7)
 AND ($9::uuid='00000000-0000-0000-0000-000000000000' OR r.id=$9)
 AND ($10='' OR c.provider=$10)
 AND ($11='' OR r.details->'snapshot'->>'source_native_id'=$11)
 AND ($12='' OR r.details->'snapshot'->>'source_type'=$12)
 ORDER BY r.id LIMIT $8`, scope, actor, after, f.Type, f.Connection, f.Region, f.Account, limit+1, target, f.Provider, f.SourceID, f.SourceType)
	if err != nil {
		return ResourcePage{}, workspace.ErrStore
	}
	defer rows.Close()
	result := ResourcePage{Data: []Resource{}}
	for rows.Next() {
		var r Resource
		var last *time.Time
		var failure *string
		if rows.Scan(&r.ID, &r.ConnectionID, &r.Provider, &r.APIFamily, &r.AccountIdentity, &r.Type, &r.Scope, &r.NativeID, &r.Name, &r.Region, &r.Status, &r.NativeStatus, &r.ObservedAt, &last, &failure, &r.details, &r.role, &r.grants, &r.connectionStatus, &r.verified) != nil {
			return ResourcePage{}, workspace.ErrStore
		}
		r.Inventory = NewFreshness(last, failure)
		result.Data = append(result.Data, r)
	}
	if rows.Err() != nil {
		return ResourcePage{}, workspace.ErrStore
	}
	if len(result.Data) > limit {
		result.Data = result.Data[:limit]
		data, _ := json.Marshal(resourceCursor{scope, actor, digest, result.Data[limit-1].ID})
		next := base64.RawURLEncoding.EncodeToString(data)
		result.NextCursor = &next
	}
	return result, nil
}
