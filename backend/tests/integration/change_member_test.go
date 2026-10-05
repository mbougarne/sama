//go:build integration

package integration

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sama/backend/internal/httpapi"
	"sama/backend/internal/identity"
	"sama/backend/internal/workspace"
	"strings"
	"testing"
	"time"
)

func TestMembershipChangesCeilingsAtomicityAndConcurrency(t *testing.T) {
	pool := identityDatabase(t)
	owner, scope := seedIdentity(t, pool)
	admin, _ := seedIdentity(t, pool)
	target, _ := seedIdentity(t, pool)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `INSERT INTO memberships VALUES($1,$2,'admin',1)`, scope, admin); err != nil {
		t.Fatal(err)
	}
	change := func(actor string, role string, version int64) error {
		id := owner
		if actor == "admin" {
			id = admin
		}
		return workspace.ChangeMember(ctx, pool, id, scope, target, role, version, "change-test")
	}
	if change("admin", "owner", 0) != workspace.ErrDenied || change("admin", "admin", 0) != workspace.ErrDenied {
		t.Fatal("admin escalation")
	}
	if err := change("admin", "viewer", 0); err != nil {
		t.Fatal(err)
	}
	if change("owner", "operator", 9) != workspace.ErrConflict {
		t.Fatal("stale version")
	}
	if _, err := pool.Exec(ctx, `ALTER TABLE audit_events ADD CONSTRAINT reject_member CHECK(event_type<>'membership.changed') NOT VALID`); err != nil {
		t.Fatal(err)
	}
	if change("owner", "operator", 2) != workspace.ErrStore {
		t.Fatal("audit failure not propagated")
	}
	var role string
	if err := pool.QueryRow(ctx, `SELECT role FROM memberships WHERE workspace_id=$1 AND user_id=$2`, scope, target).Scan(&role); err != nil || role != "viewer" {
		t.Fatal("audit rollback")
	}
	if _, err := pool.Exec(ctx, `ALTER TABLE audit_events DROP CONSTRAINT reject_member`); err != nil {
		t.Fatal(err)
	}
	sessions, _ := identity.NewSessions(pool, identity.DefaultSessionPolicy(), time.Now)
	credential := issueSession(t, sessions, pool, owner, time.Now().Add(-time.Second), "")
	handler := httpapi.NewHandler(&httpapi.Auth{Pool: pool, Sessions: sessions, OIDC: &identity.OIDC{Config: identity.OIDCConfig{PublicOrigin: "https://sama.example"}}})
	request := func(method, body, csrf string) int {
		r := httptest.NewRequest(method, "/api/v1/workspaces/"+scope.String()+"/members/"+target.String(), strings.NewReader(body))
		r.AddCookie(&http.Cookie{Name: "__Host-sama_session", Value: credential.Token})
		r.Header.Set("Origin", "https://sama.example")
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", csrf)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w.Code
	}
	if request("PUT", `{"role":"owner","version":2}`, "wrong") != 403 || request("PUT", `{"role":"owner"}`, credential.CSRF) != 400 {
		t.Fatal("mutation boundary")
	}
	if request("PUT", `{"role":"owner","version":2}`, credential.CSRF) != 204 {
		t.Fatal("owner assignment")
	}
	if change("admin", "viewer", 3) != workspace.ErrDenied {
		t.Fatal("admin changed owner")
	}
	results := make(chan error, 2)
	go func() { results <- workspace.ChangeMember(ctx, pool, owner, scope, owner, "", 1, "remove-owner") }()
	go func() { results <- workspace.ChangeMember(ctx, pool, target, scope, target, "", 3, "remove-target") }()
	one, two := <-results, <-results
	if !((one == nil && two == workspace.ErrConflict) || (two == nil && one == workspace.ErrConflict)) {
		t.Fatalf("final owner: %v %v", one, two)
	}
	var owners int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM memberships WHERE workspace_id=$1 AND role='owner'`, scope).Scan(&owners); err != nil || owners != 1 {
		t.Fatal("lost final owner")
	}
	// The next authorization lookup sees removal immediately.
	removed := owner
	if one != nil {
		removed = target
	}
	// Channel completion order does not identify which writer succeeded.
	if err := pool.QueryRow(ctx, `SELECT user_id FROM (VALUES($1::uuid),($2::uuid)) AS candidates(user_id) WHERE NOT EXISTS(SELECT 1 FROM memberships m WHERE m.workspace_id=$3 AND m.user_id=candidates.user_id)`, owner, target, scope).Scan(&removed); err != nil {
		t.Fatal(err)
	}
	if _, err := workspace.ResolveMembership(ctx, pool, removed, scope, "viewer"); err != workspace.ErrNotFound {
		t.Fatal(fmt.Sprint("revocation delayed: ", err))
	}
}

func TestReassignmentVersionsAndElevatedSessions(t *testing.T) {
	pool := identityDatabase(t)
	owner, scope := seedIdentity(t, pool)
	target, _ := seedIdentity(t, pool)
	ctx := context.Background()
	sessions, _ := identity.NewSessions(pool, identity.DefaultSessionPolicy(), time.Now)
	old := issueSession(t, sessions, pool, target, time.Now().Add(-time.Second), "")
	change := func(role string, version int64) error {
		return workspace.ChangeMember(ctx, pool, owner, scope, target, role, version, "reassignment")
	}
	if err := change("viewer", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := sessions.Resolve(ctx, old.Token); err != identity.ErrUnauthenticated {
		t.Fatal("new authority reused old session")
	}
	member, err := workspace.ResolveMembership(ctx, pool, target, scope, "viewer")
	if err != nil {
		t.Fatal(err)
	}
	if err := change("", member.Version); err != nil {
		t.Fatal(err)
	}
	if err := change("viewer", 0); err != nil {
		t.Fatal(err)
	}
	if change("operator", member.Version) != workspace.ErrConflict {
		t.Fatal("removed membership version became valid again")
	}
}
