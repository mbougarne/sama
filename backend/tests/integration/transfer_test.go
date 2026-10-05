//go:build integration

package integration

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sama/backend/internal/httpapi"
	"sama/backend/internal/identity"
	"sama/backend/internal/workspace"
	"strings"
	"testing"
	"time"
)

func TestOwnershipTransferCurrentAtomicAndConcurrent(t *testing.T) {
	pool := identityDatabase(t)
	owner, scope := seedIdentity(t, pool)
	target, _ := seedIdentity(t, pool)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `INSERT INTO memberships VALUES($1,$2,'admin',1)`, scope, target); err != nil {
		t.Fatal(err)
	}
	transfer := func(a, b int64) error {
		return workspace.Transfer(ctx, pool, owner, scope, target, a, b, true, "transfer-test")
	}
	if transfer(9, 1) != workspace.ErrConflict || transfer(1, 9) != workspace.ErrConflict {
		t.Fatal("stale transfer")
	}
	if workspace.Transfer(ctx, pool, target, scope, owner, 1, 1, true, "transfer-test") != workspace.ErrDenied {
		t.Fatal("nonowner transfer")
	}
	if _, err := pool.Exec(ctx, `ALTER TABLE audit_events ADD CONSTRAINT reject_transfer CHECK(metadata->>'role'<>'admin') NOT VALID`); err != nil {
		t.Fatal(err)
	}
	if transfer(1, 1) != workspace.ErrStore {
		t.Fatal("audit failure")
	}
	if m, err := workspace.ResolveMembership(ctx, pool, owner, scope, "owner"); err != nil || m.Version != 1 {
		t.Fatal("partial transfer")
	}
	if _, err := pool.Exec(ctx, `ALTER TABLE audit_events DROP CONSTRAINT reject_transfer`); err != nil {
		t.Fatal(err)
	}
	sessions, _ := identity.NewSessions(pool, identity.DefaultSessionPolicy(), time.Now)
	credential := issueSession(t, sessions, pool, owner, time.Now().Add(-time.Second), "")
	handler := httpapi.NewHandler(&httpapi.Auth{Pool: pool, Sessions: sessions, OIDC: &identity.OIDC{Config: identity.OIDCConfig{PublicOrigin: "https://sama.example"}}})
	r := httptest.NewRequest("POST", "/api/v1/workspaces/"+scope.String()+"/ownership-transfers", strings.NewReader(`{"user_id":"`+target.String()+`","actor_version":1,"target_version":1,"demote":true}`))
	r.AddCookie(&http.Cookie{Name: "__Host-sama_session", Value: credential.Token})
	r.Header.Set("Origin", "https://sama.example")
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-CSRF-Token", credential.CSRF)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 204 {
		t.Fatal(w.Body.String())
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE workspace_id=$1 AND event_type='membership.changed' AND actor_id=$2 AND metadata->>'member_id' IN ($2::text,$3::text)`, scope, owner, target).Scan(&count); err != nil || count != 2 {
		t.Fatal("missing affected identities", err)
	}
	results := make(chan error, 2)
	go func() { results <- workspace.Transfer(ctx, pool, target, scope, owner, 2, 2, true, "return-transfer") }()
	go func() { results <- workspace.ChangeMember(ctx, pool, target, scope, owner, "", 2, "remove-former") }()
	a, b := <-results, <-results
	if a != nil && b != nil {
		t.Fatalf("no winner %v %v", a, b)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM memberships WHERE workspace_id=$1 AND role='owner'`, scope).Scan(&count); err != nil || count < 1 {
		t.Fatal("no surviving owner")
	}
}
