//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"net/http"
	"net/http/httptest"
	"sama/backend/internal/httpapi"
	"sama/backend/internal/identity"
	"sama/backend/internal/workspace"
	"strings"
	"testing"
	"time"
)

func TestMembersScopeCeilingAndCursors(t *testing.T) {
	pool := identityDatabase(t)
	owner, scope := seedIdentity(t, pool)
	admin, _ := seedIdentity(t, pool)
	viewer, other := seedIdentity(t, pool)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `INSERT INTO memberships VALUES($1,$2,'admin',1),($1,$3,'viewer',1)`, scope, admin, viewer); err != nil {
		t.Fatal(err)
	}
	sessions, _ := identity.NewSessions(pool, identity.DefaultSessionPolicy(), time.Now)
	handler := httpapi.NewHandler(&httpapi.Auth{Pool: pool, Sessions: sessions, OIDC: &identity.OIDC{}})
	read := func(user uuid.UUID, path string) *httptest.ResponseRecorder {
		c := issueSession(t, sessions, pool, user, time.Now().Add(-time.Second), "")
		r := httptest.NewRequest("GET", path, nil)
		r.AddCookie(&http.Cookie{Name: "__Host-sama_session", Value: c.Token})
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	path := "/api/v1/workspaces/" + scope.String() + "/members"
	var page workspace.MemberPage
	w := read(owner, path+"?limit=1")
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil || w.Code != 200 || len(page.Data) != 1 || page.Data[0].UserID != owner || page.NextCursor == nil {
		t.Fatal("first page", w.Body.String())
	}
	w = read(owner, path+"?cursor="+page.NextCursor.String())
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil || len(page.Data) != 2 || page.NextCursor != nil {
		t.Fatal("stable next page")
	}
	w = read(admin, path)
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil || len(page.Data) != 1 || page.Data[0].UserID != viewer {
		t.Fatal("admin ceiling")
	}
	for _, suffix := range []string{"?limit=0", "?limit=201", "?limit=1&limit=2", "?cursor=invalid"} {
		if read(owner, path+suffix).Code != 400 {
			t.Fatal("malformed paging")
		}
	}
	if read(viewer, path).Code != 403 || read(owner, "/api/v1/workspaces/"+other.String()+"/members?cursor="+owner.String()).Code != 404 {
		t.Fatal("enumeration boundary")
	}
	for _, secret := range []string{"issuer", "subject", "token", "csrf"} {
		if strings.Contains(w.Body.String(), secret) {
			t.Fatal("unsafe metadata")
		}
	}
	// A valid forged cursor changes only position, never tenant or role visibility.
	w = read(admin, path+"?cursor="+owner.String())
	if strings.Contains(w.Body.String(), admin.String()) || strings.Contains(w.Body.String(), owner.String()) {
		t.Fatal("cursor bypassed visibility")
	}
}
