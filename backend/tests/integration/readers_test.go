//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"sama/backend/internal/httpapi"
	"sama/backend/internal/identity"
	"sama/backend/internal/workspace"
)

func TestCurrentUserAndWorkspaceListSafeAndCurrent(t *testing.T) {
	pool := identityDatabase(t)
	user, first := seedIdentity(t, pool)
	_, other := seedIdentity(t, pool)
	second := entityID(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `INSERT INTO workspaces(id,name) VALUES($1,'Second')`, second); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO memberships VALUES($1,$2,'viewer',1)`, second, user); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	sessions, err := identity.NewSessions(pool, identity.DefaultSessionPolicy(), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	credential := issueSession(t, sessions, pool, user, now, "")
	handler := httpapi.NewHandler(&httpapi.Auth{OIDC: &identity.OIDC{}, Pool: pool, Sessions: sessions})
	read := func(target string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", target, nil)
		r.AddCookie(&http.Cookie{Name: "__Host-sama_session", Value: credential.Token})
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	me := read("/api/v1/me")
	var fields map[string]any
	if err := json.Unmarshal(me.Body.Bytes(), &fields); err != nil || me.Code != 200 || len(fields) != 2 || fields["id"] != user.String() {
		t.Fatal("current user shape/UUID")
	}
	firstPage := read("/api/v1/workspaces?limit=1")
	var page workspace.Page
	if err := json.Unmarshal(firstPage.Body.Bytes(), &page); err != nil || firstPage.Code != 200 || len(page.Data) != 1 || page.Data[0].ID != first || page.Data[0].Role != "owner" || page.NextCursor == nil {
		t.Fatal("first page shape/role/UUID")
	}
	next := read("/api/v1/workspaces?limit=1&cursor=" + page.NextCursor.String())
	if err := json.Unmarshal(next.Body.Bytes(), &page); err != nil || len(page.Data) != 1 || page.Data[0].ID != second || page.Data[0].Role != "viewer" || page.NextCursor != nil {
		t.Fatal("cursor paging")
	}
	for _, target := range []string{"/api/v1/workspaces?limit=101", "/api/v1/workspaces?limit=0", "/api/v1/workspaces?cursor=invalid"} {
		if read(target).Code != 400 {
			t.Fatal("invalid pagination accepted")
		}
	}
	if _, err := pool.Exec(ctx, `DELETE FROM memberships WHERE workspace_id=$1 AND user_id=$2`, second, user); err != nil {
		t.Fatal(err)
	}
	listed := read("/api/v1/workspaces")
	if strings.Contains(listed.Body.String(), other.String()) || strings.Contains(listed.Body.String(), second.String()) {
		t.Fatal("out-of-scope or revoked membership listed")
	}
	for _, response := range []string{me.Body.String(), listed.Body.String()} {
		for _, secret := range []string{credential.Token, credential.CSRF, "token_digest", "issuer", "subject", "csrf_digest"} {
			if strings.Contains(response, secret) {
				t.Fatal("secret/internal identity fields exposed")
			}
		}
	}
	contract, err := os.ReadFile("../../api/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"required: [id, display_name]", "required: [id, name, role]", "format: uuid", "enum: [viewer, operator, admin, owner]"} {
		if !strings.Contains(string(contract), field) {
			t.Fatal("contract disagrees with DTO fields")
		}
	}
	// Canonical IDs remain opaque strings, not numbers.
	if _, err := uuid.Parse(fields["id"].(string)); err != nil {
		t.Fatal(err)
	}
}
