package operation

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"sama/backend/internal/identity"
	"sama/backend/internal/platform"
)

type Resolution struct {
	Acknowledged bool   `json:"residual_risk_acknowledged"`
	Target       string `json:"typed_target"`
}

// ReleaseUnknown records an owner decision about residual risk. The operation
// stays unknown; releasing a reservation neither proves success nor retries it.
func (s *Service) ReleaseUnknown(ctx context.Context, p identity.Principal, scope, id uuid.UUID, input Resolution, request string) (Accepted, error) {
	if s == nil || s.pool == nil {
		return Accepted{}, ErrStore
	}
	if !input.Acknowledged {
		return Accepted{}, ErrInvalid
	}
	if s.sessions == nil || s.sessions.RequireRecent(p) != nil {
		return Accepted{}, identity.ErrRecentRequired
	}
	tx, err := platform.BeginTx(ctx, s.pool, pgx.TxOptions{})
	if err != nil {
		return Accepted{}, ErrStore
	}
	defer tx.Rollback(ctx)
	pl, role, err := controlLoad(ctx, tx, p.UserID, scope, id)
	if err != nil {
		return Accepted{}, err
	}
	if role != "owner" {
		return Accepted{}, ErrDenied
	}
	if pl.state != "unknown" {
		return Accepted{}, ErrState
	}
	var name string
	var high bool
	if tx.QueryRow(ctx, `SELECT r.target_name,'high_impact'=ANY(g.actions) FROM operations o JOIN action_reviews r ON r.id=o.review_id
 JOIN connection_grants g ON (g.workspace_id,g.connection_id,g.user_id)=(o.workspace_id,o.connection_id,$3)
 WHERE o.workspace_id=$1 AND o.id=$2`, scope, id, p.UserID).Scan(&name, &high) != nil {
		return Accepted{}, ErrStore
	}
	if !high {
		return Accepted{}, ErrDenied
	}
	if name != input.Target {
		return Accepted{}, ErrInvalid
	}
	auditID, err := controlAudit(ctx, tx, pl, p.UserID, scope, request, "reservation_released_residual_risk")
	if err != nil {
		return Accepted{}, err
	}
	resolution, err := uuid.NewV7()
	if err != nil {
		return Accepted{}, ErrStore
	}
	if _, err = tx.Exec(ctx, `INSERT INTO operation_resolutions(id,workspace_id,operation_id,actor_id,audit_id,residual_risk_acknowledged) VALUES($1,$2,$3,$4,$5,true)`, resolution, scope, id, p.UserID, auditID); err != nil {
		return Accepted{}, ErrState
	}
	result, err := tx.Exec(ctx, `DELETE FROM resource_locks WHERE workspace_id=$1 AND operation_id=$2`, scope, id)
	if err != nil || result.RowsAffected() != 1 {
		return Accepted{}, ErrState
	}
	if _, err = tx.Exec(ctx, `UPDATE jobs SET state='cancelled',completed_at=clock_timestamp(),lease_owner=NULL,lease_token=NULL,lease_expires_at=NULL WHERE workspace_id=$1 AND operation_id=$2`, scope, id); err != nil {
		return Accepted{}, ErrStore
	}
	if tx.Commit(ctx) != nil {
		return Accepted{}, ErrStore
	}
	return Accepted{id, "unknown"}, nil
}
