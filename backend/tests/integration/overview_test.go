//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sama/backend/internal/httpapi"
	"sama/backend/internal/identity"
	"sama/backend/internal/workspace"
	"testing"
	"time"
)

func TestOverviewCountsGrantedPublishedObservationsAndCapsHealth(t *testing.T) {
	pool, lease, actor, target := operationFixture(t)
	ctx := context.Background()
	id := storedOperation(t, pool, lease, actor, storedReview(t, pool, lease, actor, target), target, "overview")
	if _, err := pool.Exec(ctx, `UPDATE operations SET state='unknown' WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 53; i++ {
		conn := entityID(t)
		if _, err := pool.Exec(ctx, `INSERT INTO connections(id,workspace_id,provider,api_family,account_identity,label) VALUES($1,$2,'fixture','compute','account','Additional')`, conn, lease.Workspace); err != nil {
			t.Fatal(err)
		}
		if i < 52 {
			if _, err := pool.Exec(ctx, `INSERT INTO connection_grants(workspace_id,connection_id,user_id,actions) VALUES($1,$2,$3,'{read}')`, lease.Workspace, conn, actor); err != nil {
				t.Fatal(err)
			}
		}
	}
	value, err := workspace.ReadOverview(ctx, pool, actor, lease.Workspace)
	if err != nil || value.ConnectionCount != 53 || value.ResourceCount != 1 || value.UnknownOperations != 1 || value.PendingOperations != 0 || value.StaleConnections != 52 || len(value.Connections) != 50 || !value.ConnectionsTruncated {
		t.Fatal(value, err)
	}
	if _, err = pool.Exec(ctx, `UPDATE operations SET state='waiting_provider' WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	value, err = workspace.ReadOverview(ctx, pool, actor, lease.Workspace)
	if err != nil || value.PendingOperations != 1 || value.UnknownOperations != 0 {
		t.Fatal("outcome states", value, err)
	}
	sessions, _ := identity.NewSessions(pool, identity.DefaultSessionPolicy(), time.Now)
	session := issueSession(t, sessions, pool, actor, time.Now().Add(-time.Second), "")
	handler := httpapi.NewHandler(&httpapi.Auth{Pool: pool, Sessions: sessions, OIDC: &identity.OIDC{Config: identity.OIDCConfig{PublicOrigin: "https://sama.example"}}})
	read := func(query string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/"+lease.Workspace.String()+"/overview"+query, nil)
		r.AddCookie(&http.Cookie{Name: "__Host-sama_session", Value: session.Token})
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	w := read("")
	var projected workspace.Overview
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &projected) != nil || projected.ResourceCount != 1 {
		t.Fatal("overview HTTP", w.Code)
	}
	if w = read("?provider=fixture"); w.Code != 400 {
		t.Fatal("unexpected query", w.Code)
	}
	if _, err = pool.Exec(ctx, `DELETE FROM connection_grants WHERE user_id=$1`, actor); err != nil {
		t.Fatal(err)
	}
	value, err = workspace.ReadOverview(ctx, pool, actor, lease.Workspace)
	if err != nil || value.ConnectionCount != 0 || value.ResourceCount != 0 || value.PendingOperations != 0 || len(value.Connections) != 0 {
		t.Fatal("denied totals", value, err)
	}
	if _, err = pool.Exec(ctx, `DELETE FROM memberships WHERE user_id=$1`, actor); err != nil {
		t.Fatal(err)
	}
	if _, err = workspace.ReadOverview(ctx, pool, actor, lease.Workspace); err != workspace.ErrNotFound {
		t.Fatal("lost membership rendered zero", err)
	}
	if w = read(""); w.Code != 404 {
		t.Fatal("missing membership totals", w.Code)
	}
}
