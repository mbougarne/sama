//go:build integration

package integration

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"sama/backend/internal/httpapi"
	"sama/backend/internal/identity"
	"sama/backend/internal/workspace"
)

func TestPrincipalCurrentMembershipAndTenantIsolation(t *testing.T) {
	pool := identityDatabase(t)
	user, first := seedIdentity(t, pool)
	_, second := seedIdentity(t, pool)
	now := time.Now().UTC()
	sessions, err := identity.NewSessions(pool, identity.DefaultSessionPolicy(), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	credential := issueSession(t, sessions, pool, user, now, "")
	auth := &httpapi.Auth{OIDC: &identity.OIDC{}, Sessions: sessions, Pool: pool}
	boundary := auth.Protect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, ok := httpapi.CurrentPrincipal(r.Context())
		if !ok || p.UserID != user {
			t.Error("missing principal")
		}
		m, ok := httpapi.CurrentMembership(r.Context())
		if !ok || m.WorkspaceID != first {
			t.Error("missing scoped membership")
		}
		w.WriteHeader(204)
	}), "admin")
	probe := func(path, token string) int {
		r := httptest.NewRequest("GET", path, nil)
		if token != "" {
			r.AddCookie(&http.Cookie{Name: "__Host-sama_session", Value: token})
		}
		r.Header.Set("X-Workspace-Role", "owner")
		w := httptest.NewRecorder()
		boundary.ServeHTTP(w, r)
		return w.Code
	}
	path := "/api/v1/workspaces/" + first.String() + "/test"
	if probe(path, "") != 401 || probe(path, "invalid") != 401 {
		t.Fatal("absent/invalid token admitted")
	}
	if probe(path, credential.Token) != 204 {
		t.Fatal("owner denied")
	}
	if probe("/api/v1/workspaces/"+second.String()+"/test", credential.Token) != 404 {
		t.Fatal("cross-workspace visibility")
	}
	if _, err := pool.Exec(context.Background(), `UPDATE memberships SET role='viewer',version=version+1 WHERE user_id=$1`, user); err != nil {
		t.Fatal(err)
	}
	if probe(path, credential.Token) != 403 {
		t.Fatal("role change ignored")
	}
	if _, err := pool.Exec(context.Background(), `DELETE FROM memberships WHERE user_id=$1`, user); err != nil {
		t.Fatal(err)
	}
	if probe(path, credential.Token) != 404 {
		t.Fatal("membership revocation ignored")
	}
	now = now.Add(30 * time.Minute)
	if probe(path, credential.Token) != 401 {
		t.Fatal("expired session admitted")
	}
	for _, invalid := range []string{"", "unknown", "Owner"} {
		if workspace.Allows("owner", invalid) {
			t.Fatal("unknown authority admitted")
		}
	}
}
