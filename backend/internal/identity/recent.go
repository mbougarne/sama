package identity

import (
	"context"
	"crypto/subtle"
	"errors"
	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"time"
)

var ErrRecentRequired = errors.New("recent authentication required")
var ErrReauthUnsupported = errors.New("issuer reauthentication policy is not configured or supported")

// RequireRecent is the reusable high-impact acceptance gate. Token issuance time
// alone is not authentication evidence; only a verified round trip qualifies.
func (s *Sessions) RequireRecent(p Principal) error {
	if !p.Reauthenticated || !recentAt(p.AuthenticatedAt, s.now()) {
		return ErrRecentRequired
	}
	return nil
}
func recentAt(at, now time.Time) bool {
	return !at.IsZero() && !at.After(now) && now.Sub(at) < 5*time.Minute
}

// AuthenticationTime checks signed claims from an already verified ID token.
// ACR is an installation-reviewed issuer policy, not a browser-selectable value.
func (o *OIDC) AuthenticationTime(token *oidc.IDToken, started time.Time) (time.Time, error) {
	if !o.ReauthenticationSupported {
		return time.Time{}, ErrReauthUnsupported
	}
	var claims struct {
		AuthTime int64  `json:"auth_time"`
		ACR      string `json:"acr"`
	}
	if token.Claims(&claims) != nil || claims.AuthTime <= 0 || claims.ACR != o.Config.ReauthACR {
		return time.Time{}, ErrRecentRequired
	}
	at := time.Unix(claims.AuthTime, 0)
	if !recentAt(at, o.now()) || at.Before(started.Truncate(time.Second)) {
		return time.Time{}, ErrRecentRequired
	}
	return at, nil
}

// RotateRecent checks the original active session and identity in the same
// transaction that deletes it and creates its replacement. Logout wins races.
func (s *Sessions) RotateRecent(ctx context.Context, tx pgx.Tx, user uuid.UUID, at time.Time, previous string, boundDigest []byte) (Credential, error) {
	digest, ok := tokenDigest(previous)
	if !ok || subtle.ConstantTimeCompare(digest, boundDigest) != 1 || !recentAt(at, s.now()) {
		return Credential{}, ErrUnauthenticated
	}
	deleted, err := tx.Exec(ctx, `DELETE FROM sessions WHERE token_digest=$1 AND user_id=$2 AND idle_expires_at>$3 AND absolute_expires_at>$3`, digest, user, s.now())
	if err != nil {
		return Credential{}, ErrStore
	}
	if deleted.RowsAffected() != 1 {
		return Credential{}, ErrUnauthenticated
	}
	credential, err := s.Create(ctx, tx, user, at, "")
	if err != nil {
		return Credential{}, err
	}
	next, _ := tokenDigest(credential.Token)
	if _, err := tx.Exec(ctx, `UPDATE sessions SET reauthenticated=true WHERE token_digest=$1`, next); err != nil {
		return Credential{}, ErrStore
	}
	return credential, nil
}
