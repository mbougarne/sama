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
)

func TestGrantHTTPBoundary(t *testing.T) {
	pool := identityDatabase(t)
	owner, scope := seedIdentity(t, pool)
	id := entityID(t)
	if _, err := pool.Exec(context.Background(), `INSERT INTO connections(id,workspace_id,provider,api_family,account_identity,label) VALUES($1,$2,'fixture','compute','account','Fixture')`, id, scope); err != nil {
		t.Fatal(err)
	}
	sessions, _ := identity.NewSessions(pool, identity.DefaultSessionPolicy(), time.Now)
	credential := issueSession(t, sessions, pool, owner, time.Now().Add(-time.Second), "")
	handler := httpapi.NewHandler(&httpapi.Auth{Pool: pool, Sessions: sessions, OIDC: &identity.OIDC{Config: identity.OIDCConfig{PublicOrigin: "https://sama.example"}}})
	request := func(method, body, csrf string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "/api/v1/workspaces/"+scope.String()+"/connections/"+id.String()+"/grants", strings.NewReader(body))
		r.AddCookie(&http.Cookie{Name: "__Host-sama_session", Value: credential.Token})
		r.Header.Set("Origin", "https://sama.example")
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", csrf)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	body := `{"user_id":"` + owner.String() + `","actions":["read"]}`
	if request("GET", "", "").Body.String() != "{\"actions\":[]}\n" {
		t.Fatal("default projection")
	}
	if request("PUT", body, "wrong").Code != 403 || request("PUT", `{"unknown":true}`, credential.CSRF).Code != 400 {
		t.Fatal("strict mutation boundary")
	}
	if w := request("PUT", body, credential.CSRF); w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	if request("GET", "", "").Body.String() != "{\"actions\":[\"read\"]}\n" || request("POST", body, credential.CSRF).Code != 405 {
		t.Fatal("read/method contract")
	}
}
