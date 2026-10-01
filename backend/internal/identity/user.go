package identity

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type UserView struct {
	ID          uuid.UUID `json:"id"`
	DisplayName string    `json:"display_name"`
}

func CurrentUser(ctx context.Context, pool *pgxpool.Pool, id uuid.UUID) (UserView, error) {
	var user UserView
	err := pool.QueryRow(ctx, `SELECT id,display_name FROM users WHERE id=$1 AND disabled_at IS NULL`, id).Scan(&user.ID, &user.DisplayName)
	if errors.Is(err, pgx.ErrNoRows) {
		return UserView{}, ErrUnauthenticated
	}
	if err != nil {
		return UserView{}, ErrStore
	}
	return user, nil
}
