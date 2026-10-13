package workspace

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"slices"
)

type GrantView struct {
	UserID         uuid.UUID `json:"user_id"`
	Actions        []string  `json:"actions"`
	AllowedActions []string  `json:"allowed_actions"`
	Editable       bool      `json:"editable"`
}

// ReadGrantView derives both sides' current role/grant ceilings in one snapshot.
// This is rendering guidance; SetGrant independently locks and rechecks authority.
func ReadGrantView(ctx context.Context, pool *pgxpool.Pool, actor, scope, connection, target uuid.UUID) (GrantView, error) {
	result := GrantView{UserID: target, AllowedActions: []string{}}
	var role, targetRole string
	var own []string
	err := pool.QueryRow(ctx, `SELECT m.role,t.role,COALESCE(g.actions,'{}'),COALESCE(own.actions,'{}')
 FROM workspaces w JOIN memberships m ON m.workspace_id=w.id AND m.user_id=$2
 JOIN users u ON u.id=m.user_id AND u.disabled_at IS NULL
 JOIN connections c ON c.workspace_id=w.id AND c.id=$3
 JOIN memberships t ON t.workspace_id=w.id AND t.user_id=$4
 JOIN users tu ON tu.id=t.user_id AND tu.disabled_at IS NULL
 LEFT JOIN connection_grants g ON (g.workspace_id,g.connection_id,g.user_id)=(w.id,c.id,t.user_id)
 LEFT JOIN connection_grants own ON (own.workspace_id,own.connection_id,own.user_id)=(w.id,c.id,m.user_id)
 WHERE w.id=$1 AND w.closed_at IS NULL AND m.role IN ('owner','admin')`, scope, actor, connection, target).Scan(&role, &targetRole, &result.Actions, &own)
	if errors.Is(err, pgx.ErrNoRows) {
		return GrantView{}, ErrNotFound
	}
	if err != nil {
		return GrantView{}, ErrStore
	}
	result.Editable = CanAssign(role, targetRole)
	for _, action := range []string{"read", "refresh", "operate", "high_impact"} {
		if result.Editable && WithinCeiling(targetRole, action) && (role == "owner" || slices.Contains(own, action)) {
			result.AllowedActions = append(result.AllowedActions, action)
		}
	}
	return result, nil
}
