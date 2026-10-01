//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"sama/backend/internal/httpapi"
	"sama/backend/internal/identity"
	"sama/backend/internal/workspace"
)

func TestWorkspaceCreationAtomicBoundedAndIsolated(t *testing.T) {
	pool := identityDatabase(t)
	user, _ := seedIdentity(t, pool)
	other, _ := seedIdentity(t, pool)
	now := time.Now().UTC()
	sessions, err := identity.NewSessions(pool, identity.DefaultSessionPolicy(), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	credential := issueSession(t, sessions, pool, user, now, "")
	otherCredential := issueSession(t, sessions, pool, other, now, "")
	handler := httpapi.NewHandler(&httpapi.Auth{OIDC: &identity.OIDC{Config: identity.OIDCConfig{PublicOrigin: "https://sama.example"}}, Pool: pool, Sessions: sessions})
	create := func(body, csrf string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/api/v1/workspaces", strings.NewReader(body))
		r.AddCookie(&http.Cookie{Name: "__Host-sama_session", Value: credential.Token})
		r.Header.Set("Origin", "https://sama.example")
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", csrf)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	if create(`{"name":"Valid"}`, "wrong").Code != 403 {
		t.Fatal("missing CSRF admitted")
	}
	for _, body := range []string{`{"name":""}`, `{"name":" padded "}`, `{"name":"line\nbreak"}`, `{"name":"` + strings.Repeat("a", 101) + `"}`} {
		if create(body, credential.CSRF).Code != 422 {
			t.Fatal("invalid name accepted")
		}
	}
	for _, body := range []string{`{"name":"Valid","role":"owner"}`, `{"name":"Valid"}{}`, strings.Repeat("x", 1025)} {
		if create(body, credential.CSRF).Code != 400 {
			t.Fatal("invalid/oversized body accepted")
		}
	}
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `ALTER TABLE audit_events ADD CONSTRAINT reject_workspace CHECK(event_type<>'workspace.created')`); err != nil {
		t.Fatal(err)
	}
	if create(`{"name":"Rollback"}`, credential.CSRF).Code != 503 {
		t.Fatal("audit failure committed creation")
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM workspaces`).Scan(&count); err != nil || count != 2 {
		t.Fatal("failed creation left workspace")
	}
	if _, err := pool.Exec(ctx, `ALTER TABLE audit_events DROP CONSTRAINT reject_workspace`); err != nil {
		t.Fatal(err)
	}
	response := create(`{"name":"Created workspace"}`, credential.CSRF)
	var created workspace.View
	if err := json.Unmarshal(response.Body.Bytes(), &created); err != nil || response.Code != 201 || created.Role != "owner" || response.Header().Get("Location") != "/api/v1/workspaces/"+created.ID.String() {
		t.Fatal("creation response")
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM workspaces w JOIN memberships m ON m.workspace_id=w.id JOIN audit_events a ON a.workspace_id=w.id WHERE w.id=$1 AND m.user_id=$2 AND m.role='owner' AND a.actor_id=$2`, created.ID, user).Scan(&count); err != nil || count != 1 {
		t.Fatal("workspace/ownership/audit were not atomic")
	}
	request := httptest.NewRequest("GET", "/api/v1/workspaces/"+created.ID.String()+"/test", nil)
	request.AddCookie(&http.Cookie{Name: "__Host-sama_session", Value: otherCredential.Token})
	denied := httptest.NewRecorder()
	handler.ServeHTTP(denied, request)
	if denied.Code != 404 {
		t.Fatal("other user could enter new workspace")
	}
	for range 7 {
		if create(`{"name":"Within limit"}`, credential.CSRF).Code != 201 {
			t.Fatal("quota rejected early")
		}
	}
	results := make(chan int, 2)
	for range 2 {
		go func() { results <- create(`{"name":"Concurrent last slot"}`, credential.CSRF).Code }()
	}
	one, two := <-results, <-results
	if !((one == 201 && two == 429) || (one == 429 && two == 201)) {
		t.Fatalf("concurrent quota bypass: %d %d", one, two)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM memberships WHERE user_id=$1 AND role='owner'`, user).Scan(&count); err != nil || count != workspace.MaxOwnedWorkspaces {
		t.Fatal("owned-workspace limit")
	}
}
