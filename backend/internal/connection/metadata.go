// Package connection owns stored connection identity and credential metadata.
package connection

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("connection not found")
var ErrStore = errors.New("connection store unavailable")

// Metadata is the read projection. Credential material has no field here.
// Storage does not enable a provider or authorize an adapter call.
type Metadata struct {
	ID                      uuid.UUID  `json:"id"`
	WorkspaceID             uuid.UUID  `json:"workspace_id"`
	Provider                string     `json:"provider"`
	APIFamily               string     `json:"api_family"`
	AccountIdentity         string     `json:"account_identity"`
	RegionPolicy            []string   `json:"region_policy"`
	Label                   string     `json:"label"`
	Status                  string     `json:"status"`
	ActiveCredentialVersion *int64     `json:"active_credential_version"`
	PermissionsVerifiedAt   *time.Time `json:"permissions_verified_at"`
}

// ReadMetadata requires prior caller authorization; it always scopes by workspace.
// Future HTTP list/grant services must enforce their own connection permissions.
func ReadMetadata(ctx context.Context, pool *pgxpool.Pool, workspaceID, connectionID uuid.UUID) (Metadata, error) {
	var result Metadata
	err := pool.QueryRow(ctx, `SELECT id,workspace_id,provider,api_family,account_identity,
        region_policy,label,status,active_credential_version,permissions_verified_at
        FROM connections WHERE workspace_id=$1 AND id=$2`, workspaceID, connectionID).Scan(
		&result.ID, &result.WorkspaceID, &result.Provider, &result.APIFamily, &result.AccountIdentity,
		&result.RegionPolicy, &result.Label, &result.Status, &result.ActiveCredentialVersion, &result.PermissionsVerifiedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Metadata{}, ErrNotFound
	}
	if err != nil {
		return Metadata{}, ErrStore
	}
	return result, nil
}
