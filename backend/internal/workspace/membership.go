package workspace

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("workspace unavailable")
var ErrDenied = errors.New("workspace permission denied")

type Membership struct {
	WorkspaceID uuid.UUID
	UserID      uuid.UUID
	Role        string
	Version     int64
}

// ResolveMembership always reads current database authority, including closure.
// A globally valid UUID is not sufficient to enter another workspace.
func ResolveMembership(ctx context.Context, pool *pgxpool.Pool, user, workspace uuid.UUID, minimumRole string) (Membership, error) {
	var m Membership
	err := pool.QueryRow(ctx, `SELECT m.workspace_id,m.user_id,m.role,m.version FROM memberships m
 JOIN workspaces w ON w.id=m.workspace_id WHERE m.workspace_id=$1 AND m.user_id=$2 AND w.closed_at IS NULL`, workspace, user).Scan(&m.WorkspaceID, &m.UserID, &m.Role, &m.Version)
	if errors.Is(err, pgx.ErrNoRows) {
		return Membership{}, ErrNotFound
	}
	if err != nil {
		return Membership{}, ErrStore
	}
	if !Allows(m.Role, minimumRole) {
		return Membership{}, ErrDenied
	}
	return m, nil
}

func Allows(role, minimum string) bool {
	ranks := map[string]int{"viewer": 1, "operator": 2, "admin": 3, "owner": 4}
	return ranks[minimum] > 0 && ranks[role] >= ranks[minimum]
}
