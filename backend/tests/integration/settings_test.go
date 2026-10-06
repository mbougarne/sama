//go:build integration

package integration

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sama/backend/internal/httpapi"
	"sama/backend/internal/identity"
	"sama/backend/internal/workspace"
	"strings"
	"testing"
	"time"
)

func TestSettingsPolicyAtomicityAndAuthority(t *testing.T) {
	pool := identityDatabase(t)
	owner, scope := seedIdentity(t, pool)
	outsider, _ := seedIdentity(t, pool)
	ctx := context.Background()
	get := func() workspace.Settings {
		t.Helper()
		s, err := workspace.WorkspaceSettings(ctx, pool, owner, scope, nil, "settings")
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	initial := get()
	if initial.PolicyVersion != 1 || initial.QueueLimit != 1000 || initial.AuditRetentionDays != 180 {
		t.Fatal(initial)
	}
	change := func(s workspace.Settings) error {
		_, err := workspace.WorkspaceSettings(ctx, pool, owner, scope, &s, "settings")
		return err
	}
	for _, s := range []workspace.Settings{{"", 1, 10, 180}, {"Name", 1, 0, 180}, {"Name", 1, 1001, 180}, {"Name", 1, 10, 179}, {"Name", 1, 10, 3651}} {
		if change(s) != workspace.ErrInvalid {
			t.Fatal("unsafe policy accepted")
		}
	}
	if _, err := workspace.WorkspaceSettings(ctx, pool, outsider, scope, nil, "settings"); err != workspace.ErrNotFound {
		t.Fatal("cross-tenant access")
	}
	if _, err := pool.Exec(ctx, `INSERT INTO memberships VALUES($1,$2,'admin',1)`, scope, outsider); err != nil {
		t.Fatal(err)
	}
	if _, err := workspace.WorkspaceSettings(ctx, pool, outsider, scope, &initial, "settings"); err != workspace.ErrDenied {
		t.Fatal("admin policy write")
	}
	if _, err := pool.Exec(ctx, `ALTER TABLE audit_events ADD CONSTRAINT reject_settings CHECK(event_type<>'workspace.settings_changed') NOT VALID`); err != nil {
		t.Fatal(err)
	}
	updated := workspace.Settings{Name: "Renamed", PolicyVersion: 1, QueueLimit: 50, AuditRetentionDays: 365}
	if change(updated) != workspace.ErrStore || get() != initial {
		t.Fatal("audit did not roll back policy")
	}
	if _, err := pool.Exec(ctx, `ALTER TABLE audit_events DROP CONSTRAINT reject_settings`); err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 2)
	go func() { results <- change(updated) }()
	go func() { results <- change(updated) }()
	a, b := <-results, <-results
	if !((a == nil && b == workspace.ErrPolicyConflict) || (b == nil && a == workspace.ErrPolicyConflict)) {
		t.Fatalf("versions: %v %v", a, b)
	}
	current := get()
	if current.PolicyVersion != 2 || current.Name != "Renamed" {
		t.Fatal(current)
	}
	if err := change(current); err != nil || get() != current {
		t.Fatal("no-op changed version")
	}
	var audits int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE event_type='workspace.settings_changed'`).Scan(&audits); err != nil || audits != 1 {
		t.Fatal("audit count", audits, err)
	}
	m, err := workspace.ResolveMembership(ctx, pool, owner, scope, "owner")
	if err != nil || m.Version != 1 {
		t.Fatal("policy changed membership")
	}
	sessions, _ := identity.NewSessions(pool, identity.DefaultSessionPolicy(), time.Now)
	credential := issueSession(t, sessions, pool, owner, time.Now().Add(-time.Second), "")
	handler := httpapi.NewHandler(&httpapi.Auth{Pool: pool, Sessions: sessions, OIDC: &identity.OIDC{Config: identity.OIDCConfig{PublicOrigin: "https://sama.example"}}})
	for _, body := range []string{`{"unknown":true}`, `{"name":"Name","policy_version":2,"queue_limit":10,"audit_retention_days":180,"role":"owner"}`} {
		r := httptest.NewRequest("PUT", "/api/v1/workspaces/"+scope.String()+"/settings", strings.NewReader(body))
		r.AddCookie(&http.Cookie{Name: "__Host-sama_session", Value: credential.Token})
		r.Header.Set("Origin", "https://sama.example")
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", credential.CSRF)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != 400 {
			t.Fatal(fmt.Sprint("unknown fields: ", w.Code))
		}
	}
}
