package connection

import (
	"context"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"sama/backend/internal/provider"
	"sama/backend/internal/workspace"
)

type View struct {
	Metadata
	Capabilities []provider.Capability `json:"capabilities"`
}

type Page struct {
	Data       []View     `json:"data"`
	NextCursor *uuid.UUID `json:"next_cursor"`
}

var catalog = []provider.Support{
	{Action: "compute.server.read", Class: "read"},
	{Action: "compute.server.power_on", Class: "operate"},
	{Action: "compute.server.shutdown", Class: "operate"},
	{Action: "compute.server.power_off", Class: "operate"},
	{Action: "compute.server.reboot", Class: "operate"},
}

// ReadAuthorized filters membership and grants in the same snapshot as metadata.
// implemented is a trusted adapter registry projection; an empty registry denies
// support. Verified timestamps alone never imply permission for another action.
func ReadAuthorized(ctx context.Context, pool *pgxpool.Pool, actor, scope, cursor, target uuid.UUID, limit int, implemented map[string][]string) (Page, error) {
	if limit < 1 || limit > 200 {
		return Page{}, workspace.ErrInvalid
	}
	rows, err := pool.Query(ctx, `SELECT c.id,c.workspace_id,c.provider,c.api_family,c.account_identity,
        c.region_policy,c.label,c.status,c.active_credential_version,c.permissions_verified_at,
        c.verified_capabilities,m.role,g.actions
        FROM connections c JOIN workspaces w ON w.id=c.workspace_id AND w.closed_at IS NULL
        JOIN memberships m ON m.workspace_id=w.id AND m.user_id=$2
        JOIN users u ON u.id=m.user_id AND u.disabled_at IS NULL
        JOIN connection_grants g ON (g.workspace_id,g.connection_id,g.user_id)=(w.id,c.id,m.user_id)
        WHERE c.workspace_id=$1 AND 'read'=ANY(g.actions) AND c.id>$3
        AND ($4::uuid='00000000-0000-0000-0000-000000000000' OR c.id=$4)
        ORDER BY c.id LIMIT $5`, scope, actor, cursor, target, limit+1)
	if err != nil {
		return Page{}, workspace.ErrStore
	}
	defer rows.Close()
	page := Page{Data: []View{}}
	for rows.Next() {
		var view View
		var verified, grants []string
		var role string
		m := &view.Metadata
		if rows.Scan(&m.ID, &m.WorkspaceID, &m.Provider, &m.APIFamily, &m.AccountIdentity, &m.RegionPolicy, &m.Label, &m.Status, &m.ActiveCredentialVersion, &m.PermissionsVerifiedAt, &verified, &role, &grants) != nil {
			return Page{}, workspace.ErrStore
		}
		if m.PermissionsVerifiedAt == nil {
			verified = nil
		}
		grants = slices.DeleteFunc(grants, func(action string) bool { return !workspace.WithinCeiling(role, action) })
		state := []string{"compute.server.read"}
		// Resource-specific mutation restrictions are unknown in a connection view.
		view.Capabilities = provider.Effective(catalog, implemented[m.Provider+"/"+m.APIFamily], verified, grants, state)
		page.Data = append(page.Data, view)
	}
	if rows.Err() != nil {
		return Page{}, workspace.ErrStore
	}
	if len(page.Data) > limit {
		page.Data = page.Data[:limit]
		id := page.Data[limit-1].ID
		page.NextCursor = &id
	}
	if target != uuid.Nil && len(page.Data) == 0 {
		return Page{}, workspace.ErrNotFound
	}
	return page, nil
}
