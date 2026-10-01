package identity

import (
	"context"
	"crypto/subtle"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"sama/backend/internal/audit"
	"sama/backend/internal/platform"
)

func ValidCSRF(p Principal, raw string) bool {
	digest, ok := tokenDigest(raw)
	return ok && subtle.ConstantTimeCompare(digest, p.CSRFHash) == 1
}

// Logout retains audit scope even if membership has since been revoked.
// Revocation and audit are committed together; repeated logout writes no duplicate.
func (s *Sessions) Logout(ctx context.Context, token, requestID string) error {
	digest, ok := tokenDigest(token)
	if !ok {
		return nil
	}
	tx, err := platform.BeginTx(ctx, s.pool, pgx.TxOptions{})
	if err != nil {
		return ErrStore
	}
	defer tx.Rollback(ctx)
	var user, workspace uuid.UUID
	err = tx.QueryRow(ctx, `DELETE FROM sessions WHERE token_digest=$1 RETURNING user_id,audit_workspace_id`, digest).Scan(&user, &workspace)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return ErrStore
	}
	id, err := audit.NewID()
	if err != nil {
		return ErrStore
	}
	if err := audit.Append(ctx, tx, audit.Event{ID: id, Workspace: workspace, ActorKind: audit.ActorUser, ActorID: &user, Type: "auth.logout", RequestID: requestID}); err != nil {
		return ErrStore
	}
	if err := tx.Commit(ctx); err != nil {
		return ErrStore
	}
	return nil
}

// Cleanup processes one bounded batch and cannot remove an unexpired session.
func (s *Sessions) Cleanup(ctx context.Context, limit int) (int64, error) {
	if limit < 1 || limit > 1000 {
		return 0, errors.New("invalid session cleanup limit")
	}
	tag, err := platform.Exec(ctx, s.pool, `DELETE FROM sessions WHERE token_digest IN
 (SELECT token_digest FROM sessions WHERE idle_expires_at<=$1 OR absolute_expires_at<=$1 ORDER BY idle_expires_at LIMIT $2)`, s.now().UTC(), limit)
	if err != nil {
		return 0, ErrStore
	}
	return tag.RowsAffected(), nil
}
