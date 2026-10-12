//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"sama/backend/internal/connection"
	"sama/backend/internal/httpapi"
	"sama/backend/internal/identity"
	"sama/backend/internal/inventory"
	"sama/backend/internal/job"
	"sama/backend/internal/provider"
	"sama/backend/internal/workspace"
)

func inventoryHTTP(t *testing.T, pool *pgxpool.Pool, actor uuid.UUID, support ...map[string][]string) func(string) *httptest.ResponseRecorder {
	t.Helper()
	sessions, _ := identity.NewSessions(pool, identity.DefaultSessionPolicy(), time.Now)
	token := issueSession(t, sessions, pool, actor, time.Now().Add(-time.Second), "")
	registry := map[string][]string{}
	if len(support) > 0 {
		registry = support[0]
	}
	handler := httpapi.NewHandler(&httpapi.Auth{ConnectionSupport: registry, Pool: pool, Sessions: sessions, OIDC: &identity.OIDC{Config: identity.OIDCConfig{PublicOrigin: "https://sama.example"}}})
	return func(path string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, path, nil)
		r.AddCookie(&http.Cookie{Name: "__Host-sama_session", Value: token.Token})
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
}

func TestSyncStatusKeepsLastSuccessAndFiltersCurrentAuthority(t *testing.T) {
	pool, lease, actor, run := inventoryLease(t)
	ctx := context.Background()
	request := inventoryHTTP(t, pool, actor)
	path := "/api/v1/workspaces/" + lease.Workspace.String() + "/syncs/" + run.String()
	w := request(path)
	var pending inventory.SyncStatus
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &pending) != nil || pending.Status != "pending" || pending.Inventory.State != "never_observed" {
		t.Fatal("pending response", w.Code, w.Body.String())
	}
	scanner := inventory.Scanner{Pool: pool, Reader: func(context.Context, provider.Binding) (provider.InventoryReader, error) {
		return scanReader{func(context.Context, string) (provider.InventoryPage, error) { return provider.InventoryPage{}, nil }}, nil
	}}
	if err := scanner.Execute(ctx, lease); err != nil {
		t.Fatal(err)
	}
	last := time.Now().Add(-time.Second).UTC()
	if _, err := pool.Exec(ctx, `UPDATE sync_runs SET completed_at=$2 WHERE id=$1`, run, last); err != nil {
		t.Fatal(err)
	}
	failed, err := inventory.Refresh(ctx, pool, actor, lease.Workspace, lease.Connection)
	if err != nil {
		t.Fatal(err)
	}
	next, err := (job.Scheduler{Pool: pool, Now: func() time.Time { return time.Now().Add(time.Second) }}).Claim(ctx, entityID(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := scanner.Complete(ctx, next, errors.New("untrusted secret body")); err != nil {
		t.Fatal(err)
	}
	w = request("/api/v1/workspaces/" + lease.Workspace.String() + "/syncs/" + failed.String())
	var status inventory.SyncStatus
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &status) != nil || status.Inventory.State != "stale" || status.Inventory.LastSuccessAt == nil || status.Inventory.LastSuccessAt.Sub(last) > time.Microsecond || status.FailureCode == nil || *status.FailureCode != "provider_unavailable" {
		t.Fatal("failure freshness", w.Code, w.Body.String())
	}
	page, err := connection.ReadAuthorized(ctx, pool, actor, lease.Workspace, uuid.Nil, lease.Connection, 1, nil)
	if err != nil || page.Data[0].Inventory.State != "stale" || page.Data[0].Inventory.LastSuccessAt == nil {
		t.Fatal("connection freshness", err)
	}
	_, other := seedIdentity(t, pool)
	if _, err = inventory.Status(ctx, pool, actor, other, failed); err != workspace.ErrNotFound {
		t.Fatal("tenant scope escaped", err)
	}
	if _, err = pool.Exec(ctx, `DELETE FROM connection_grants WHERE user_id=$1`, actor); err != nil {
		t.Fatal(err)
	}
	if request(path).Code != 404 || request("/api/v1/workspaces/"+lease.Workspace.String()+"/syncs/"+entityID(t).String()).Code != 404 {
		t.Fatal("missing and denied differ")
	}
}
