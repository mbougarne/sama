package audit

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
	"time"
)

var ErrReadInvalid = errors.New("invalid audit query")
var ErrReadStore = errors.New("audit history unavailable")

type Filters struct {
	Actor  uuid.UUID
	Action string
	Target uuid.UUID
}
type Entry struct {
	ID        uuid.UUID         `json:"id"`
	ActorKind ActorKind         `json:"actor_kind"`
	ActorID   *uuid.UUID        `json:"actor_id"`
	Action    string            `json:"action"`
	CreatedAt time.Time         `json:"created_at"`
	Metadata  map[string]string `json:"metadata"`
}
type Page struct {
	Data       []Entry `json:"data"`
	NextCursor *string `json:"next_cursor"`
}
type cursor struct {
	Workspace uuid.UUID
	Actor     uuid.UUID
	Filter    string
	At        time.Time
	ID        uuid.UUID
}

// List reads only allowed fields, with current tenant and connection grants in
// the same snapshot. Filters narrow this authorization; they never confer it.
func List(ctx context.Context, pool *pgxpool.Pool, actor, scope uuid.UUID, f Filters, position string, limit int) (Page, error) {
	if limit < 1 || limit > 200 || f.Action != "" && !safeCode.MatchString(f.Action) || f.Actor != uuid.Nil && !isEntityID(f.Actor) || f.Target != uuid.Nil && !isEntityID(f.Target) {
		return Page{}, ErrReadInvalid
	}
	b, _ := json.Marshal(f)
	hash := sha256.Sum256(b)
	digest := hex.EncodeToString(hash[:])
	after := cursor{Workspace: scope, Actor: actor, Filter: digest}
	if position != "" {
		if len(position) > 1024 {
			return Page{}, ErrReadInvalid
		}
		b, err := base64.RawURLEncoding.DecodeString(position)
		if err != nil {
			return Page{}, ErrReadInvalid
		}
		decoder := json.NewDecoder(bytes.NewReader(b))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&after) != nil || decoder.Decode(new(any)) != io.EOF || after.Workspace != scope || after.Actor != actor || after.Filter != digest || !isEntityID(after.ID) || after.At.IsZero() {
			return Page{}, ErrReadInvalid
		}
	}
	rows, err := pool.Query(ctx, `SELECT a.id,a.actor_kind,a.actor_id,a.event_type,a.created_at,a.metadata
 FROM audit_events a JOIN workspaces w ON w.id=a.workspace_id AND w.closed_at IS NULL
 JOIN memberships m ON m.workspace_id=w.id AND m.user_id=$2
 JOIN users u ON u.id=m.user_id AND u.disabled_at IS NULL
 WHERE a.workspace_id=$1 AND ($4::uuid='00000000-0000-0000-0000-000000000000' OR (a.created_at,a.id)<($3,$4))
 AND ($5::uuid='00000000-0000-0000-0000-000000000000' OR a.actor_id=$5)
 AND ($6='' OR a.event_type=$6)
 AND ($7::uuid='00000000-0000-0000-0000-000000000000' OR $7::text IN (a.metadata->>'connection_id',a.metadata->>'operation_id',a.metadata->>'resource_id',a.metadata->>'member_id'))
 AND CASE WHEN a.event_type LIKE 'connection.%' THEN EXISTS(
  SELECT 1 FROM connection_grants g WHERE g.workspace_id=w.id AND g.user_id=m.user_id AND g.connection_id::text=a.metadata->>'connection_id' AND 'read'=ANY(g.actions))
 WHEN a.event_type LIKE 'operation.%' THEN EXISTS(
  SELECT 1 FROM operations o JOIN connection_grants g ON (g.workspace_id,g.connection_id,g.user_id)=(o.workspace_id,o.connection_id,m.user_id)
  WHERE o.workspace_id=w.id AND o.id::text=a.metadata->>'operation_id' AND 'read'=ANY(g.actions))
 ELSE m.role IN ('owner','admin') OR a.actor_id=m.user_id END
 AND (NOT (a.metadata ? 'resource_id') OR EXISTS(SELECT 1 FROM resources r JOIN connection_grants g
  ON (g.workspace_id,g.connection_id,g.user_id)=(r.workspace_id,r.connection_id,m.user_id)
  WHERE r.workspace_id=w.id AND r.id::text=a.metadata->>'resource_id' AND 'read'=ANY(g.actions)))
 ORDER BY a.created_at DESC,a.id DESC LIMIT $8`, scope, actor, after.At, after.ID, f.Actor, f.Action, f.Target, limit+1)
	if err != nil {
		return Page{}, ErrReadStore
	}
	defer rows.Close()
	result := Page{Data: []Entry{}}
	for rows.Next() {
		var e Entry
		var metadata map[string]string
		if rows.Scan(&e.ID, &e.ActorKind, &e.ActorID, &e.Action, &e.CreatedAt, &metadata) != nil {
			return Page{}, ErrReadStore
		}
		e.Metadata = map[string]string{}
		for field, value := range metadata {
			if metadataFields[e.Action][field] {
				e.Metadata[field] = value
			}
		}
		result.Data = append(result.Data, e)
	}
	if rows.Err() != nil {
		return Page{}, ErrReadStore
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
