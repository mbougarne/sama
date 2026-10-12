package inventory

import (
	"context"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"sama/backend/internal/provider"
	"sama/backend/internal/workspace"
)

type ResourceDetail struct {
	Resource
	DetailsVersion   int                   `json:"details_version"`
	Details          Details               `json:"details"`
	AvailableActions []provider.Capability `json:"available_actions"`
}

// Detail shares the list's tenant/grant snapshot. Adapter support comes only
// from the trusted registry; this projection never authorizes future dispatch.
func Detail(ctx context.Context, pool *pgxpool.Pool, actor, scope, id uuid.UUID, implemented map[string][]string) (ResourceDetail, error) {
	if id.Version() != 7 {
		return ResourceDetail{}, workspace.ErrInvalid
	}
	page, err := query(ctx, pool, actor, scope, Filters{}, "", 1, id)
	if err != nil {
		return ResourceDetail{}, err
	}
	if len(page.Data) == 0 {
		return ResourceDetail{}, workspace.ErrNotFound
	}
	r := page.Data[0]
	grants := slices.DeleteFunc(r.grants, func(class string) bool { return !workspace.WithinCeiling(r.role, class) })
	allowed := []string{"compute.server.read"}
	if r.connectionStatus == "active" {
		if r.Status == "stopped" {
			allowed = append(allowed, "compute.server.power_on")
		}
		if r.Status == "running" {
			allowed = append(allowed, "compute.server.shutdown", "compute.server.power_off", "compute.server.reboot")
		}
	}
	capabilities := provider.Effective(provider.ServerCatalog, implemented[r.Provider+"/"+r.APIFamily], r.verified, grants, allowed)
	return ResourceDetail{r, 1, r.details, capabilities}, nil
}
