package workspace

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Member struct {
	UserID      uuid.UUID `json:"user_id"`
	DisplayName string    `json:"display_name"`
	Role        string    `json:"role"`
	Version     int64     `json:"version"`
}
type MemberPage struct {
	Data       []Member   `json:"data"`
	NextCursor *uuid.UUID `json:"next_cursor"`
}

// Owner/admin membership administration exposes only safe identity metadata.
// A cursor is a position, never authority; every query reapplies tenant policy.
func ListMembers(ctx context.Context, pool *pgxpool.Pool, actor, scope, cursor uuid.UUID, limit int) (MemberPage, error) {
	page := MemberPage{Data: []Member{}}
	if limit < 1 || limit > 200 {
		return page, ErrInvalid
	}
	if _, err := ResolveMembership(ctx, pool, actor, scope, "admin"); err != nil {
		return page, err
	}
	rows, err := pool.Query(ctx, `SELECT m.user_id,u.display_name,m.role,m.version FROM memberships m
 JOIN users u ON u.id=m.user_id JOIN workspaces w ON w.id=m.workspace_id
 JOIN memberships a ON a.workspace_id=m.workspace_id AND a.user_id=$1
 WHERE m.workspace_id=$2 AND w.closed_at IS NULL AND a.role IN ('owner','admin')
 AND (a.role='owner' OR m.role IN ('viewer','operator')) AND m.user_id>$3 ORDER BY m.user_id LIMIT $4`, actor, scope, cursor, limit+1)
	if err != nil {
		return page, ErrStore
	}
	defer rows.Close()
	for rows.Next() {
		var item Member
		if err := rows.Scan(&item.UserID, &item.DisplayName, &item.Role, &item.Version); err != nil {
			return page, ErrStore
		}
		page.Data = append(page.Data, item)
	}
	if rows.Err() != nil {
		return page, ErrStore
	}
	if len(page.Data) > limit {
		page.Data = page.Data[:limit]
		last := page.Data[limit-1].UserID
		page.NextCursor = &last
	}
	return page, nil
}
