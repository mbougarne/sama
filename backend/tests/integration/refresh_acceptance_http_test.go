//go:build integration

package integration

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"sama/backend/internal/httpapi"
	"sama/backend/internal/identity"
	"sama/backend/internal/workspace"
)

func TestRefreshHTTPRequiresCurrentGrantAndReturnsAcceptedReference(t *testing.T) {
	pool := identityDatabase(t)
	actor, scope := seedIdentity(t, pool)
	ctx := context.Background()
	id := entityID(t)
	for _, statement := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO connections(id,workspace_id,provider,api_family,account_identity,label) VALUES($1,$2,'fixture','compute','account','Fixture')`, []any{id, scope}},
		{`INSERT INTO credential_versions(workspace_id,connection_id,version,ciphertext,nonce,wrapped_data_key,wrapping_nonce,encryption_format,master_key_id) VALUES($1,$2,1,'c','n','k','w',1,'fixture')`, []any{scope, id}},
		{`UPDATE connections SET status='active',active_credential_version=1 WHERE id=$1`, []any{id}},
	} {
		if _, err := pool.Exec(ctx, statement.sql, statement.args...); err != nil {
			t.Fatal(err)
		}
	}
	sessions, _ := identity.NewSessions(pool, identity.DefaultSessionPolicy(), time.Now)
	token := issueSession(t, sessions, pool, actor, time.Now().Add(-time.Second), "")
	handler := httpapi.NewHandler(&httpapi.Auth{Pool: pool, Sessions: sessions, OIDC: &identity.OIDC{Config: identity.OIDCConfig{PublicOrigin: "https://sama.example"}}})
	request := func(body, csrf string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/api/v1/workspaces/"+scope.String()+"/connections/"+id.String()+"/syncs", strings.NewReader(body))
		r.AddCookie(&http.Cookie{Name: "__Host-sama_session", Value: token.Token})
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", "https://sama.example")
		r.Header.Set("X-CSRF-Token", csrf)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	if request("{}", "wrong").Code != 403 || request("{}", token.CSRF).Code != 404 {
		t.Fatal("mutation or grant protection")
	}
	if err := workspace.SetGrant(ctx, pool, actor, scope, id, actor, []string{"read", "refresh"}, "grant"); err != nil {
		t.Fatal(err)
	}
	first, second := request("{}", token.CSRF), request("{}", token.CSRF)
	if first.Code != 202 || second.Code != 202 || first.Body.String() != second.Body.String() {
		t.Fatal("coalesced 202", first.Code, second.Code)
	}
	if request(`{"token":"synthetic-secret"}`, token.CSRF).Code != 400 {
		t.Fatal("unknown body accepted")
	}
}
