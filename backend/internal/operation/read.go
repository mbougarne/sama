package operation

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
	"sama/backend/internal/workspace"
	"time"
)

type EventView struct {
	Sequence  int64     `json:"sequence"`
	State     string    `json:"state"`
	CreatedAt time.Time `json:"created_at"`
}
type View struct {
	ID                uuid.UUID   `json:"id"`
	ConnectionID      uuid.UUID   `json:"connection_id"`
	ResourceID        uuid.UUID   `json:"resource_id"`
	ActorID           uuid.UUID   `json:"actor_id"`
	Action            string      `json:"action"`
	State             string      `json:"state"`
	Uncertain         bool        `json:"uncertain"`
	TargetName        string      `json:"target_name"`
	NativeID          string      `json:"native_id"`
	Provider          string      `json:"provider"`
	AccountIdentity   string      `json:"account_identity"`
	Region            string      `json:"region"`
	ProviderReference *string     `json:"provider_reference"`
	CreatedAt         time.Time   `json:"created_at"`
	UpdatedAt         time.Time   `json:"updated_at"`
	Events            []EventView `json:"events"`
}
type Page struct {
	Data       []View  `json:"data"`
	NextCursor *string `json:"next_cursor"`
}
type historyCursor struct {
	Workspace uuid.UUID
	Actor     uuid.UUID
	At        time.Time
	ID        uuid.UUID
}

// Read is a local snapshot of current authority and durable evidence. Disabled
// connections retain granted history; HTTP success never changes stored state.
func Read(ctx context.Context, pool *pgxpool.Pool, actor, scope, target uuid.UUID, position string, limit int) (Page, error) {
	if limit < 1 || limit > 200 {
		return Page{}, workspace.ErrInvalid
	}
	after := historyCursor{Workspace: scope, Actor: actor}
	if position != "" {
		if len(position) > 1024 || target != uuid.Nil {
			return Page{}, workspace.ErrInvalid
		}
		b, err := base64.RawURLEncoding.DecodeString(position)
		if err != nil {
			return Page{}, workspace.ErrInvalid
		}
		d := json.NewDecoder(bytes.NewReader(b))
		d.DisallowUnknownFields()
		if d.Decode(&after) != nil || d.Decode(new(any)) != io.EOF || after.Workspace != scope || after.Actor != actor || after.At.IsZero() || after.ID.Version() != 7 {
			return Page{}, workspace.ErrInvalid
		}
	}
	rows, err := pool.Query(ctx, `SELECT o.id,o.connection_id,o.resource_id,o.actor_id,o.action,o.state,ar.target_name,
 r.native_id,c.provider,c.account_identity,COALESCE(r.details->>'region',''),o.provider_reference,o.created_at,o.updated_at,
 COALESCE((SELECT jsonb_agg(e ORDER BY e.sequence) FROM
 (SELECT sequence,CASE WHEN to_state IN ('queued','dispatching','waiting_provider','reconciling','unknown','succeeded','failed','cancelled') THEN to_state ELSE 'unknown' END AS state,created_at
  FROM operation_events WHERE workspace_id=o.workspace_id AND operation_id=o.id ORDER BY sequence DESC LIMIT 20) e),'[]'::jsonb)
 FROM operations o JOIN workspaces w ON w.id=o.workspace_id AND w.closed_at IS NULL
 JOIN memberships m ON m.workspace_id=w.id AND m.user_id=$2 JOIN users u ON u.id=m.user_id AND u.disabled_at IS NULL
 JOIN connections c ON (c.workspace_id,c.id)=(w.id,o.connection_id)
 JOIN resources r ON (r.workspace_id,r.connection_id,r.id)=(w.id,c.id,o.resource_id)
 JOIN action_reviews ar ON (ar.workspace_id,ar.connection_id,ar.id)=(w.id,c.id,o.review_id)
 JOIN connection_grants g ON (g.workspace_id,g.connection_id,g.user_id)=(w.id,c.id,m.user_id)
 WHERE o.workspace_id=$1 AND 'read'=ANY(g.actions)
 AND ($4::uuid='00000000-0000-0000-0000-000000000000' OR (o.created_at,o.id)<($3,$4))
 AND ($5::uuid='00000000-0000-0000-0000-000000000000' OR o.id=$5)
 ORDER BY o.created_at DESC,o.id DESC LIMIT $6`, scope, actor, after.At, after.ID, target, limit+1)
	if err != nil {
		return Page{}, workspace.ErrStore
	}
	defer rows.Close()
	result := Page{Data: []View{}}
	for rows.Next() {
		var v View
		if rows.Scan(&v.ID, &v.ConnectionID, &v.ResourceID, &v.ActorID, &v.Action, &v.State, &v.TargetName, &v.NativeID, &v.Provider, &v.AccountIdentity, &v.Region, &v.ProviderReference, &v.CreatedAt, &v.UpdatedAt, &v.Events) != nil {
			return Page{}, workspace.ErrStore
		}
		v.Uncertain = v.State == "unknown"
		result.Data = append(result.Data, v)
	}
	if rows.Err() != nil {
		return Page{}, workspace.ErrStore
	}
	if target != uuid.Nil && len(result.Data) == 0 {
		return Page{}, workspace.ErrNotFound
	}
	if len(result.Data) > limit {
		result.Data = result.Data[:limit]
		last := result.Data[limit-1]
		after.At = last.CreatedAt
		after.ID = last.ID
		b, _ := json.Marshal(after)
		next := base64.RawURLEncoding.EncodeToString(b)
		result.NextCursor = &next
	}
	return result, nil
}
