// Package identity owns OIDC identities and opaque server-side sessions.
package identity

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"sama/backend/internal/identifier"
)

var ErrUnauthenticated = errors.New("session unavailable")
var ErrStore = errors.New("identity storage unavailable")

type SessionPolicy struct{ Idle, Absolute time.Duration }

func DefaultSessionPolicy() SessionPolicy {
	return SessionPolicy{Idle: 30 * time.Minute, Absolute: 12 * time.Hour}
}

type Sessions struct {
	pool   *pgxpool.Pool
	policy SessionPolicy
	now    func() time.Time
}

type Principal struct {
	UserID          uuid.UUID
	AuthenticatedAt time.Time
	CSRFHash        []byte
}

type Credential struct{ Token, CSRF string }

func NewSessions(pool *pgxpool.Pool, policy SessionPolicy, now func() time.Time) (*Sessions, error) {
	if pool == nil || now == nil || policy.Idle < time.Second || policy.Absolute < policy.Idle || policy.Idle > 24*time.Hour || policy.Absolute > 30*24*time.Hour {
		return nil, errors.New("invalid session policy")
	}
	return &Sessions{pool: pool, policy: policy, now: now}, nil
}

func randomToken() (string, error) {
	var bytes [32]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", errors.New("randomness unavailable")
	}
	return base64.RawURLEncoding.EncodeToString(bytes[:]), nil
}

func tokenDigest(token string) ([]byte, bool) {
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(decoded) != 32 || base64.RawURLEncoding.EncodeToString(decoded) != token {
		return nil, false
	}
	digest := sha256.Sum256([]byte(token))
	return digest[:], true
}

// Create participates in the caller's transaction, allowing login and audit to
// commit together. Rotation can revoke only the same user's previous token.
func (s *Sessions) Create(ctx context.Context, tx pgx.Tx, user uuid.UUID, authenticatedAt time.Time, previous string) (Credential, error) {
	now := s.now().UTC()
	if _, err := identifier.Parse(user.String()); err != nil || authenticatedAt.After(now) || authenticatedAt.IsZero() {
		return Credential{}, ErrUnauthenticated
	}
	token, err := randomToken()
	if err != nil {
		return Credential{}, err
	}
	csrf, err := randomToken()
	if err != nil {
		return Credential{}, err
	}
	digest, _ := tokenDigest(token)
	csrfHash, _ := tokenDigest(csrf)
	if previousHash, ok := tokenDigest(previous); ok {
		if _, err := tx.Exec(ctx, `DELETE FROM sessions WHERE token_digest=$1 AND user_id=$2`, previousHash, user); err != nil {
			return Credential{}, ErrStore
		}
	}
	_, err = tx.Exec(ctx, `INSERT INTO sessions (token_digest,user_id,csrf_digest,authenticated_at,created_at,last_seen_at,idle_expires_at,absolute_expires_at,audit_workspace_id)
 SELECT $1,id,$3,$4,$5,$5,$6,$7,(SELECT workspace_id FROM memberships WHERE user_id=$2 ORDER BY workspace_id LIMIT 1) FROM users WHERE id=$2 AND disabled_at IS NULL`, digest, user, csrfHash, authenticatedAt, now, now.Add(s.policy.Idle), now.Add(s.policy.Absolute))
	if err != nil {
		return Credential{}, ErrStore
	}
	// A missing or disabled identity must not yield a usable credential.
	var present bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM sessions WHERE token_digest=$1)`, digest).Scan(&present); err != nil {
		return Credential{}, ErrStore
	}
	if !present {
		return Credential{}, ErrUnauthenticated
	}
	return Credential{Token: token, CSRF: csrf}, nil
}

// Resolve atomically checks both deadlines and slides idle expiry without
// extending absolute expiry. Membership is deliberately resolved separately.
func (s *Sessions) Resolve(ctx context.Context, token string) (Principal, error) {
	digest, ok := tokenDigest(token)
	if !ok {
		return Principal{}, ErrUnauthenticated
	}
	now := s.now().UTC()
	var p Principal
	err := s.pool.QueryRow(ctx, `UPDATE sessions s SET last_seen_at=GREATEST(s.last_seen_at,$2),
 idle_expires_at=LEAST(s.absolute_expires_at,GREATEST(s.idle_expires_at,$3))
 FROM users u WHERE s.token_digest=$1 AND u.id=s.user_id AND u.disabled_at IS NULL
 AND s.idle_expires_at>$2 AND s.absolute_expires_at>$2
 RETURNING s.user_id,s.authenticated_at,s.csrf_digest`, digest, now, now.Add(s.policy.Idle)).Scan(&p.UserID, &p.AuthenticatedAt, &p.CSRFHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return Principal{}, ErrUnauthenticated
	}
	if err != nil {
		return Principal{}, ErrStore
	}
	return p, nil
}

func (s *Sessions) Revoke(ctx context.Context, token string) error {
	digest, ok := tokenDigest(token)
	if !ok {
		return nil
	}
	if _, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE token_digest=$1`, digest); err != nil {
		return ErrStore
	}
	return nil
}

func (s *Sessions) CookieMaxAge() int { return int(s.policy.Absolute / time.Second) }
