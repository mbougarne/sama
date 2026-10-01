package workspace

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type View struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
	Role string    `json:"role"`
}
type Page struct {
	Data       []View     `json:"data"`
	NextCursor *uuid.UUID `json:"next_cursor"`
}

func List(ctx context.Context, pool *pgxpool.Pool, user, cursor uuid.UUID, limit int) (Page, error) {
	page := Page{Data: []View{}}
	if limit < 1 || limit > 100 {
		return page, ErrInvalid
	}
	rows, err := pool.Query(ctx, `SELECT w.id,w.name,m.role FROM memberships m JOIN workspaces w ON w.id=m.workspace_id
 WHERE m.user_id=$1 AND w.closed_at IS NULL AND w.id>$2 ORDER BY w.id LIMIT $3`, user, cursor, limit+1)
	if err != nil {
		return page, ErrStore
	}
	defer rows.Close()
	for rows.Next() {
		var item View
		if err := rows.Scan(&item.ID, &item.Name, &item.Role); err != nil {
			return Page{}, ErrStore
		}
		page.Data = append(page.Data, item)
	}
	if rows.Err() != nil {
		return Page{}, ErrStore
	}
	if len(page.Data) > limit {
		page.Data = page.Data[:limit]
		last := page.Data[limit-1].ID
		page.NextCursor = &last
	}
	return page, nil
}
