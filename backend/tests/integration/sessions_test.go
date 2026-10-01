//go:build integration

package integration

import (
	"context"
	"crypto/sha256"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"sama/backend/internal/identity"
)

func issueSession(t *testing.T, s *identity.Sessions, pool interface {
	Begin(context.Context) (pgx.Tx, error)
}, user uuid.UUID, now time.Time, previous string) identity.Credential {
	t.Helper()
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	credential, err := s.Create(ctx, tx, user, now, previous)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	return credential
}

func TestSessionStorageExpiryRotationAndRevocation(t *testing.T) {
	pool := identityDatabase(t)
	user, _ := seedIdentity(t, pool)
	other, _ := seedIdentity(t, pool)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	s, err := identity.NewSessions(pool, identity.DefaultSessionPolicy(), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	first := issueSession(t, s, pool, user, now, "")
	others := issueSession(t, s, pool, other, now, "")
	var digest []byte
	var idle, absolute time.Time
	if err := pool.QueryRow(context.Background(), `SELECT token_digest,idle_expires_at,absolute_expires_at FROM sessions WHERE user_id=$1`, user).Scan(&digest, &idle, &absolute); err != nil {
		t.Fatal(err)
	}
	expected := sha256.Sum256([]byte(first.Token))
	if string(digest) != string(expected[:]) || len(first.Token) != 43 || len(first.CSRF) != 43 || first.Token == first.CSRF || idle.Sub(now) != 30*time.Minute || absolute.Sub(now) != 12*time.Hour {
		t.Fatal("session storage/entropy/defaults")
	}
	second := issueSession(t, s, pool, user, now, first.Token)
	if _, err := s.Resolve(context.Background(), first.Token); !errors.Is(err, identity.ErrUnauthenticated) {
		t.Fatal("rotated session accepted")
	}
	if _, err := s.Resolve(context.Background(), others.Token); err != nil {
		t.Fatal("rotation affected another user")
	}
	if err := s.Revoke(context.Background(), second.Token); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Resolve(context.Background(), second.Token); !errors.Is(err, identity.ErrUnauthenticated) {
		t.Fatal("revoked session accepted")
	}
	idleToken := issueSession(t, s, pool, user, now, "")
	now = now.Add(30 * time.Minute)
	if _, err := s.Resolve(context.Background(), idleToken.Token); !errors.Is(err, identity.ErrUnauthenticated) {
		t.Fatal("idle deadline accepted")
	}
	absoluteToken := issueSession(t, s, pool, user, now, "")
	for range 47 {
		now = now.Add(15 * time.Minute)
		if _, err := s.Resolve(context.Background(), absoluteToken.Token); err != nil {
			t.Fatal("active session expired early")
		}
	}
	now = now.Add(15 * time.Minute)
	if _, err := s.Resolve(context.Background(), absoluteToken.Token); !errors.Is(err, identity.ErrUnauthenticated) {
		t.Fatal("absolute deadline accepted")
	}
	custom, err := identity.NewSessions(pool, identity.SessionPolicy{Idle: time.Minute, Absolute: time.Hour}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	token := issueSession(t, custom, pool, user, now, "")
	now = now.Add(time.Minute)
	if _, err := custom.Resolve(context.Background(), token.Token); !errors.Is(err, identity.ErrUnauthenticated) {
		t.Fatal("custom policy ignored")
	}
	if _, err := identity.NewSessions(pool, identity.SessionPolicy{}, time.Now); err == nil {
		t.Fatal("invalid policy accepted")
	}
}
