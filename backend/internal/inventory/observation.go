// Package inventory owns safe provider observations and refresh scopes.
package inventory

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var ErrObservation = errors.New("invalid inventory observation")

type Details struct {
	Region      string `json:"region,omitempty"`
	MachineType string `json:"machine_type,omitempty"`
}

type Observation struct {
	ID           uuid.UUID
	Workspace    uuid.UUID
	Connection   uuid.UUID
	Family       string
	Scope        string
	NativeID     string
	NativeStatus string
	Status       string
	Name         string
	Generation   uuid.UUID
	ObservedAt   time.Time
	Details      Details
}

// Upsert preserves the existing Sama identity; details are an allowlisted schema.
// Only the later complete-generation publisher may infer deletions/tombstones.
func Upsert(ctx context.Context, tx pgx.Tx, o Observation) (uuid.UUID, error) {
	if o.ID.Version() != 7 || o.Generation.Version() != 7 {
		return uuid.Nil, ErrObservation
	}
	details, err := json.Marshal(o.Details)
	if err != nil || len(details) > 65536 {
		return uuid.Nil, ErrObservation
	}
	var id uuid.UUID
	err = tx.QueryRow(ctx, `INSERT INTO resources(id,workspace_id,connection_id,api_family,resource_type,scope,native_id,native_status,status,name,details_version,details,observed_at,generation)
        SELECT $1,$2,$3,$4,'compute.server',$5,$6,$7,$8,$9,1,$10,$11,$12
        FROM connections WHERE workspace_id=$2 AND id=$3 AND api_family=$4
        ON CONFLICT(workspace_id,connection_id,api_family,resource_type,scope,native_id)
        DO UPDATE SET native_status=EXCLUDED.native_status,status=EXCLUDED.status,name=EXCLUDED.name,
        details=EXCLUDED.details,observed_at=EXCLUDED.observed_at,generation=EXCLUDED.generation,
        tombstoned_at=NULL,version=resources.version+1 RETURNING id`, o.ID, o.Workspace, o.Connection, o.Family, o.Scope, o.NativeID, o.NativeStatus, o.Status, o.Name, details, o.ObservedAt, o.Generation).Scan(&id)
	if err != nil {
		return uuid.Nil, ErrObservation
	}
	return id, nil
}
