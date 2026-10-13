//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sama/backend/internal/httpapi"
	"sama/backend/internal/identity"
	"sama/backend/internal/operation"
	"testing"
	"time"
)

func TestOperationReadsHTTPPreserveOutcomeAndHideUnscopedIDs(t *testing.T) {
	pool, lease, actor, target := operationFixture(t)
	id := storedOperation(t, pool, lease, actor, storedReview(t, pool, lease, actor, target), target, "http-read")
	if _, err := pool.Exec(context.Background(), `UPDATE operations SET state='unknown' WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	sessions, _ := identity.NewSessions(pool, identity.DefaultSessionPolicy(), time.Now)
	session := issueSession(t, sessions, pool, actor, time.Now().Add(-time.Second), "")
	handler := httpapi.NewHandler(&httpapi.Auth{Pool: pool, Sessions: sessions, OIDC: &identity.OIDC{Config: identity.OIDCConfig{PublicOrigin: "https://sama.example"}}})
	read := func(path string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/"+lease.Workspace.String()+"/operations"+path, nil)
		r.AddCookie(&http.Cookie{Name: "__Host-sama_session", Value: session.Token})
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	w := read("/" + id.String())
	var body operation.View
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &body) != nil || body.State != "unknown" || !body.Uncertain {
		t.Fatal("HTTP overwrote evidence", w.Code, w.Body.String())
	}
	if w = read("/" + entityID(t).String()); w.Code != 404 {
		t.Fatal("unknown ID", w.Code)
	}
	for _, q := range []string{"?limit=201", "?limit=1&limit=2", "?cursor=bad", "?unknown=x", "/" + id.String() + "?limit=1"} {
		if w = read(q); w.Code != 400 {
			t.Fatal("strict query", q, w.Code)
		}
	}
	if _, err := pool.Exec(context.Background(), `DELETE FROM connection_grants WHERE user_id=$1`, actor); err != nil {
		t.Fatal(err)
	}
	if w = read("/" + id.String()); w.Code != 404 {
		t.Fatal("revoked ID", w.Code)
	}
	var page operation.Page
	if w = read(""); w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &page) != nil || len(page.Data) != 0 {
		t.Fatal("revoked list", w.Code)
	}
}
