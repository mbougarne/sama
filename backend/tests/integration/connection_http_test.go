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

	"sama/backend/internal/connection"
	"sama/backend/internal/httpapi"
	"sama/backend/internal/identity"
	"sama/backend/internal/workspace"
)

func TestConnectionReadAndDisableHTTP(t *testing.T) {
	pool := identityDatabase(t)
	owner, scope := seedIdentity(t, pool)
	id := entityID(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `INSERT INTO connections(id,workspace_id,provider,api_family,account_identity,label) VALUES($1,$2,'fixture','compute','safe-account','Fixture')`, id, scope); err != nil {
		t.Fatal(err)
	}
	sessions, _ := identity.NewSessions(pool, identity.DefaultSessionPolicy(), time.Now)
	token := issueSession(t, sessions, pool, owner, time.Now().Add(-time.Second), "")
	handler := httpapi.NewHandler(&httpapi.Auth{Pool: pool, Sessions: sessions, OIDC: &identity.OIDC{Config: identity.OIDCConfig{PublicOrigin: "https://sama.example"}}})
	request := func(method, suffix, body, csrf string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "/api/v1/workspaces/"+scope.String()+"/connections"+suffix, strings.NewReader(body))
		r.AddCookie(&http.Cookie{Name: "__Host-sama_session", Value: token.Token})
		r.Header.Set("Origin", "https://sama.example")
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", csrf)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	if request("GET", "/"+id.String(), "", "").Code != 404 {
		t.Fatal("owner implicit read")
	}
	if err := workspace.SetGrant(ctx, pool, owner, scope, id, owner, []string{"read"}, "grant"); err != nil {
		t.Fatal(err)
	}
	response := request("GET", "?limit=1", "", "")
	var page connection.Page
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &page) != nil || len(page.Data) != 1 || page.Data[0].ID != id {
		t.Fatal("list contract", response.Body.String())
	}
	if request("GET", "?limit=201", "", "").Code != 400 || request("GET", "?cursor=1", "", "").Code != 400 || request("GET", "?limit=1&limit=2", "", "").Code != 400 {
		t.Fatal("pagination boundary")
	}
	response = request("GET", "/"+id.String()+"/capabilities", "", "")
	if response.Code != 200 || !strings.Contains(response.Body.String(), `"reason":"unsupported"`) {
		t.Fatal("capability contract", response.Body.String())
	}
	suffix := "/" + id.String() + "/disable"
	if request("POST", suffix, `{}`, "wrong").Code != 403 || request("POST", suffix, `{"delete":true}`, token.CSRF).Code != 400 {
		t.Fatal("disable boundary")
	}
	response = request("POST", suffix, `{}`, token.CSRF)
	if response.Code != 200 || !strings.Contains(response.Body.String(), "not revoked") {
		t.Fatal("disable explanation", response.Body.String())
	}
	if request("GET", "/"+id.String(), "", "").Code != 200 {
		t.Fatal("disabled history hidden")
	}
}
