package workspace

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"sama/backend/internal/identifier"
	"time"
)

// RedeemInvitation must run after verified OIDC exchange in the same transaction
// as session creation and login audit. Invalid proofs never admit identities.
func RedeemInvitation(ctx context.Context, tx pgx.Tx, digest []byte, issuer, subject, request string, now time.Time) error {
	var scope, inviter uuid.UUID
	var expected int64
	var role string
	err := tx.QueryRow(ctx, `SELECT workspace_id,inviter_id,inviter_version,role FROM invitations WHERE token_digest=$1 AND issuer=$2 AND subject=$3 AND expires_at>$4`, digest, issuer, subject, now).Scan(&scope, &inviter, &expected, &role)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrDenied
	}
	if err != nil {
		return ErrStore
	}
	caller, err := LockAuthority(ctx, tx, inviter, scope)
	if err != nil {
		return err
	}
	if caller.Version != expected || !CanAssign(caller.Role, role) {
		return ErrDenied
	}
	removed, err := tx.Exec(ctx, `DELETE FROM invitations WHERE token_digest=$1 AND expires_at>$2`, digest, now)
	if err != nil {
		return ErrStore
	}
	if removed.RowsAffected() != 1 {
		return ErrDenied
	}
	id, err := identifier.New()
	if err != nil {
		return ErrStore
	}
	if _, err := tx.Exec(ctx, `INSERT INTO users(id,issuer,subject) VALUES($1,$2,$3) ON CONFLICT(issuer,subject) DO NOTHING`, id, issuer, subject); err != nil {
		return ErrStore
	}
	var user uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT id FROM users WHERE issuer=$1 AND subject=$2 AND disabled_at IS NULL FOR SHARE`, issuer, subject).Scan(&user); err != nil {
		return ErrDenied
	}
	var revision int64
	if err := tx.QueryRow(ctx, `UPDATE workspaces SET policy_version=policy_version+1 WHERE id=$1 RETURNING policy_version`, scope).Scan(&revision); err != nil {
		return ErrStore
	}
	// Existing members must use the explicit version-checked membership endpoint.
	inserted, err := tx.Exec(ctx, `INSERT INTO memberships(workspace_id,user_id,role,version) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`, scope, user, role, revision)
	if err != nil {
		return ErrStore
	}
	if inserted.RowsAffected() != 1 {
		return ErrConflict
	}
	return memberAudit(ctx, tx, inviter, scope, user, role, request)
}
