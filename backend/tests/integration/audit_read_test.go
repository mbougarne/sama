//go:build integration

package integration

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sama/backend/internal/audit"
	"sama/backend/internal/httpapi"
	"sama/backend/internal/identity"
	"testing"
	"time"
)

func TestAuditReadFiltersCurrentGrantsAndRetainsHistoricalActor(t *testing.T) {
	pool, lease, actor, _ := inventoryLease(t)
	ctx := context.Background()
	historical, _ := seedIdentity(t, pool)
	if _, err := pool.Exec(ctx, `INSERT INTO memberships(workspace_id,user_id,role) VALUES($1,$2,'viewer')`, lease.Workspace, historical); err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if err = audit.Append(ctx, tx, audit.Event{ID: entityID(t), Workspace: lease.Workspace, ActorKind: audit.ActorUser, ActorID: &historical, Type: "connection.disabled", RequestID: "audit-test", Metadata: map[string]string{"connection_id": lease.Connection.String()}}); err != nil {
			t.Fatal(err)
		}
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `UPDATE audit_events SET created_at='2026-10-01T00:00:00Z' WHERE event_type='connection.disabled'`); err != nil {
		t.Fatal(err)
	}
	if _,err=pool.Exec(ctx,`DELETE FROM memberships WHERE workspace_id=$1 AND user_id=$2`,lease.Workspace,historical);err!=nil {t.Fatal(err)}
	filters := audit.Filters{Action: "connection.disabled", Actor: historical, Target: lease.Connection}
	first, err := audit.List(ctx, pool, actor, lease.Workspace, filters, "", 2)
	if err != nil || len(first.Data) != 2 || first.NextCursor == nil || first.Data[0].ActorID == nil || *first.Data[0].ActorID != historical {
		t.Fatal(first, err)
	}
	second, err := audit.List(ctx, pool, actor, lease.Workspace, filters, *first.NextCursor, 2)
	if err != nil || len(second.Data) != 1 || second.NextCursor != nil || first.Data[1].ID.String() <= second.Data[0].ID.String() {
		t.Fatal("timestamp/id cursor", second, err)
	}
	if len(first.Data[0].Metadata) != 1 {
		t.Fatal("unsafe metadata")
	}
	if _, err = audit.List(ctx, pool, actor, lease.Workspace, audit.Filters{}, *first.NextCursor, 2); err != audit.ErrReadInvalid {
		t.Fatal("changed filter cursor", err)
	}
	if _, err = pool.Exec(ctx, `DELETE FROM connection_grants WHERE user_id=$1`, actor); err != nil {
		t.Fatal(err)
	}
	denied, err := audit.List(ctx, pool, actor, lease.Workspace, filters, "", 2)
	if err != nil || len(denied.Data) != 0 {
		t.Fatal("denied target filter inferred connection", err)
	}
	denied, err = audit.List(ctx, pool, actor, lease.Workspace, filters, *first.NextCursor, 2)
	if err != nil || len(denied.Data) != 0 {
		t.Fatal("stale cursor kept authority", err)
	}
	sessions, _ := identity.NewSessions(pool, identity.DefaultSessionPolicy(), time.Now)
	session := issueSession(t, sessions, pool, actor, time.Now().Add(-time.Second), "")
	handler := httpapi.NewHandler(&httpapi.Auth{Pool: pool, Sessions: sessions, OIDC: &identity.OIDC{Config: identity.OIDCConfig{PublicOrigin:"https://sama.example"}}})
	for _, q := range []string{"?limit=201", "?actor_id=invalid", "?unknown=x", "?action=connection.disabled&action=x", "?cursor=bad"} {
		r := httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/"+lease.Workspace.String()+"/audit-events"+q, nil)
		r.AddCookie(&http.Cookie{Name: "__Host-sama_session", Value: session.Token})
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != 400 {
			t.Fatal("strict audit query", q, w.Code)
		}
	}
}
