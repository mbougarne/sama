// Package operation owns immutable reviewed intent and durable operation state.
package operation

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var ErrInvalid = errors.New("invalid operation intent")
var ErrStore = errors.New("operation store unavailable")
var ErrConflict = errors.New("operation conflict")
var ErrStale = errors.New("stale action review")
var ErrDenied = errors.New("operation permission denied")

// AppendEvent participates in the state transaction; sequence is an explicit
// operation counter and never inferred from UUID ordering.
func AppendEvent(ctx context.Context, tx pgx.Tx, workspace, id uuid.UUID, sequence int64, from, to string) error {
	event, err := uuid.NewV7()
	if err != nil || sequence < 1 {
		return ErrStore
	}
	_, err = tx.Exec(ctx, `INSERT INTO operation_events(id,workspace_id,operation_id,sequence,from_state,to_state) VALUES($1,$2,$3,$4,$5,$6)`, event, workspace, id, sequence, from, to)
	if err != nil {
		return ErrStore
	}
	return nil
}
