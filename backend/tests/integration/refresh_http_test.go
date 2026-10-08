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

func TestConnectionWritesHTTPRejectsBrowserAuthorityAndReturnsSafeReferences(t *testing.T) {
	pool := identityDatabase(t)
	actor, scope := seedIdentity(t, pool)
	ctx := context.Background()
	reader := &validationReader{account: "safe-account"}
	service := connection.NewValidationService(pool, writeKeyring(t), true, map[string]connection.Validator{"fixture/compute": reader})
	sessions, _ := identity.NewSessions(pool, identity.DefaultSessionPolicy(), time.Now)
	token := issueSession(t, sessions, pool, actor, time.Now().Add(-time.Second), "")
	handler := httpapi.NewHandler(&httpapi.Auth{Pool: pool, Sessions: sessions, ConnectionValidation: service, OIDC: &identity.OIDC{Config: identity.OIDCConfig{PublicOrigin: "https://sama.example"}}})
	request := func(path, body, csrf string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest("POST", "/api/v1/workspaces/"+scope.String()+path, strings.NewReader(body))
		r.AddCookie(&http.Cookie{Name: "__Host-sama_session", Value: token.Token})
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", "https://sama.example")
		r.Header.Set("X-CSRF-Token", csrf)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if strings.Contains(w.Body.String(), "synthetic-secret-sentinel") {
			t.Fatal("response contains credential")
		}
		return w
	}
	body := `{"family":"fixture/compute","label":"Fixture","credential":{"type":"bearer_v1","token":"synthetic-secret-sentinel"}}`
	if request("/connections", body, "wrong").Code != 403 {
		t.Fatal("missing mutation protection")
	}
	if request("/connections", strings.Replace(body, `"label"`, `"account_identity"`, 1), token.CSRF).Code != 400 {
		t.Fatal("browser authority accepted")
	}
	w := request("/connections", body, token.CSRF)
	var saved connection.Saved
	if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &saved) != nil {
		t.Fatal(w.Code, w.Body.String())
	}
	path := "/connections/" + saved.ID.String() + "/syncs"
	if request(path, "{}", token.CSRF).Code != 404 {
		t.Fatal("implicit refresh grant")
	}
	if err := workspace.SetGrant(ctx, pool, actor, scope, saved.ID, actor, []string{"read", "refresh"}, "grant"); err != nil {
		t.Fatal(err)
	}
	w = request(path, "{}", token.CSRF)
	if w.Code != 202 || !strings.Contains(w.Body.String(), saved.SyncID.String()) {
		t.Fatal("coalesced reference", w.Code, w.Body.String())
	}
	if request(path, `{"scope":"browser-chosen"}`, token.CSRF).Code != 400 {
		t.Fatal("unqualified scope accepted")
	}
	w = request("/connections/"+saved.ID.String()+"/credential-rotations", `{"credential":{"type":"bearer_v1","token":"synthetic-secret-sentinel"}}`, token.CSRF)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"version":2`) {
		t.Fatal(w.Code, w.Body.String())
	}
}
