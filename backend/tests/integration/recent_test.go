//go:build integration

package integration

import (
	"context"
	"sama/backend/internal/identity"
	"sama/backend/internal/workspace"
	"testing"
	"time"
)

func TestRecentAuthenticationAndAtomicRotation(t *testing.T) {
	pool := identityDatabase(t)
	user, scope := seedIdentity(t, pool)
	other, _ := seedIdentity(t, pool)
	ctx := context.Background()
	now := time.Now()
	sessions, _ := identity.NewSessions(pool, identity.DefaultSessionPolicy(), func() time.Time { return now })
	for _, p := range []identity.Principal{{AuthenticatedAt: now}, {Reauthenticated: true, AuthenticatedAt: now.Add(-5 * time.Minute)}, {Reauthenticated: true, AuthenticatedAt: now.Add(time.Second)}} {
		if sessions.RequireRecent(p) != identity.ErrRecentRequired {
			t.Fatal("unproven/stale/future accepted")
		}
	}
	if sessions.RequireRecent(identity.Principal{Reauthenticated: true, AuthenticatedAt: now.Add(-299 * time.Second)}) != nil {
		t.Fatal("fresh auth rejected")
	}
	old := issueSession(t, sessions, pool, user, now, "")
	digest, _ := workspace.InvitationDigest(old.Token)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sessions.RotateRecent(ctx, tx, other, now, old.Token, digest); err != identity.ErrUnauthenticated {
		t.Fatal("identity switched")
	}
	tx.Rollback(ctx)
	tx, _ = pool.Begin(ctx)
	c, err := sessions.RotateRecent(ctx, tx, user, now, old.Token, digest)
	if err != nil {
		t.Fatal(err)
	}
	tx.Rollback(ctx)
	if _, err := sessions.Resolve(ctx, c.Token); err != identity.ErrUnauthenticated {
		t.Fatal("rollback left new session")
	}
	if _, err := sessions.Resolve(ctx, old.Token); err != nil {
		t.Fatal("rollback deleted old session")
	}
	tx, _ = pool.Begin(ctx)
	c, err = sessions.RotateRecent(ctx, tx, user, now, old.Token, digest)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := sessions.Resolve(ctx, old.Token); err != identity.ErrUnauthenticated {
		t.Fatal("old session survived")
	}
	p, err := sessions.Resolve(ctx, c.Token)
	if err != nil || sessions.RequireRecent(p) != nil || c.CSRF == old.CSRF {
		t.Fatal("fresh session/CSRF rotation")
	}
	if _, err := workspace.ResolveMembership(ctx, pool, p.UserID, scope, "owner"); err != nil {
		t.Fatal("authority lost")
	}
	tx, _ = pool.Begin(ctx)
	defer tx.Rollback(ctx)
	if _, err := sessions.RotateRecent(ctx, tx, user, now, old.Token, digest); err != identity.ErrUnauthenticated {
		t.Fatal("revoked session revived")
	}
}
