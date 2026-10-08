// Package job owns durable job records. This slice does not claim or execute jobs.
package job

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var ErrJob = errors.New("invalid or unavailable durable job")

// Record carries references only. Version 1 permits connection_refresh; adding
// operation/sync payloads requires a new version and migration, never reinterpretation.
type Record struct {
	ID             uuid.UUID
	Workspace      uuid.UUID
	Connection     uuid.UUID
	Type           string
	PayloadVersion int
	Deadline       time.Time
}

// Insert stores in the caller's transaction; admission, quotas, coalescing and
// authority belong to the later refresh-acceptance service, not this storage port.
func Insert(ctx context.Context, tx pgx.Tx, record Record) error {
	if record.ID.Version() != 7 || record.Workspace.Version() != 7 || record.Connection.Version() != 7 || record.Type != "connection_refresh" || record.PayloadVersion != 1 {
		return ErrJob
	}
	_, err := tx.Exec(ctx, `INSERT INTO jobs(id,workspace_id,connection_id,type,payload_version,payload,deadline)
        VALUES($1,$2,$3,$4,$5,jsonb_build_object('connection_id',$3::uuid::text),$6)`, record.ID, record.Workspace, record.Connection, record.Type, record.PayloadVersion, record.Deadline)
	if err != nil {
		return ErrJob
	}
	return nil
}
