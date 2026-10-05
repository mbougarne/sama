package workspace

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"sama/backend/internal/audit"
	"sama/backend/internal/platform"
	"time"
)

func InvitationDigest(proof string) ([]byte, bool) {
	raw, err := base64.RawURLEncoding.DecodeString(proof)
	if err != nil || len(raw) != 32 || base64.RawURLEncoding.EncodeToString(raw) != proof {
		return nil, false
	}
	sum := sha256.Sum256([]byte(proof))
	return sum[:], true
}

// IssueInvitation returns the only plaintext copy for manual sharing. The issuer
// is installation configuration; the subject is an exact identity, never email.
func IssueInvitation(ctx context.Context, pool *pgxpool.Pool, actor, scope uuid.UUID, issuer, subject, role, request string, now time.Time) (string, error) {
	if len(issuer) == 0 || len(issuer) > 2048 || len(subject) == 0 || len(subject) > 255 || !Allows(role, "viewer") {
		return "", ErrInvalid
	}
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", ErrStore
	}
	proof := base64.RawURLEncoding.EncodeToString(raw[:])
	digest, _ := InvitationDigest(proof)
	tx, err := platform.BeginTx(ctx, pool, pgx.TxOptions{})
	if err != nil {
		return "", ErrStore
	}
	defer tx.Rollback(ctx)
	caller, err := LockAuthority(ctx, tx, actor, scope)
	if err != nil {
		return "", err
	}
	if !CanAssign(caller.Role, role) {
		return "", ErrDenied
	}
	if _, err := tx.Exec(ctx, `DELETE FROM invitations WHERE workspace_id=$1 AND expires_at<=$2`, scope, now); err != nil {
		return "", ErrStore
	}
	var count int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM invitations WHERE workspace_id=$1`, scope).Scan(&count); err != nil {
		return "", ErrStore
	}
	if count >= 100 {
		return "", ErrLimit
	}
	if _, err := tx.Exec(ctx, `INSERT INTO invitations VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, digest, scope, actor, caller.Version, issuer, subject, role, now.Add(24*time.Hour)); err != nil {
		return "", ErrStore
	}
	id, err := audit.NewID()
	if err != nil {
		return "", ErrStore
	}
	if err := audit.Append(ctx, tx, audit.Event{ID: id, Workspace: scope, ActorKind: audit.ActorUser, ActorID: &actor, Type: "invitation.created", RequestID: request, Metadata: map[string]string{"role": role}}); err != nil {
		return "", ErrStore
	}
	if err := tx.Commit(ctx); err != nil {
		return "", ErrStore
	}
	return proof, nil
}
