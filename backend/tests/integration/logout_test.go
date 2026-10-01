//go:build integration

package integration

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"sama/backend/internal/httpapi"
	"sama/backend/internal/identity"
)

func TestLogoutCSRFIsolationAuditAndBoundedCleanup(t *testing.T) {
	pool := identityDatabase(t)
	user, _ := seedIdentity(t, pool)
	other, _ := seedIdentity(t, pool)
	now := time.Now().UTC()
	sessions, err := identity.NewSessions(pool, identity.DefaultSessionPolicy(), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	first := issueSession(t, sessions, pool, user, now, "")
	second := issueSession(t, sessions, pool, other, now, "")
	handler := httpapi.NewHandler(&httpapi.Auth{OIDC: &identity.OIDC{Config: identity.OIDCConfig{PublicOrigin: "https://sama.example"}}, Sessions: sessions, Pool: pool})
	probe := func(origin, csrf string) *httptest.ResponseRecorder {
		request := httptest.NewRequest("POST", "/auth/logout", nil)
		request.AddCookie(&http.Cookie{Name: "__Host-sama_session", Value: first.Token})
		request.Header.Set("Origin", origin)
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-CSRF-Token", csrf)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		return recorder
	}
	if probe("https://attacker.example", first.CSRF).Code != 403 || probe("https://sama.example", second.CSRF).Code != 403 {
		t.Fatal("forged logout accepted")
	}
	if _, err := pool.Exec(context.Background(), `ALTER TABLE audit_events ADD CONSTRAINT reject_logout CHECK(event_type<>'auth.logout')`); err != nil {
		t.Fatal(err)
	}
	if probe("https://sama.example", first.CSRF).Code != 503 {
		t.Fatal("audit failure accepted")
	}
	if _, err := sessions.Resolve(context.Background(), first.Token); err != nil {
		t.Fatal("audit failure committed revocation")
	}
	if _, err := pool.Exec(context.Background(), `ALTER TABLE audit_events DROP CONSTRAINT reject_logout`); err != nil {
		t.Fatal(err)
	}
	// The retained session scope allows logout after all memberships are removed.
	if _, err := pool.Exec(context.Background(), `DELETE FROM memberships WHERE user_id=$1`, user); err != nil {
		t.Fatal(err)
	}
	response := probe("https://sama.example", first.CSRF)
	if response.Code != 204 || response.Result().Cookies()[0].MaxAge != -1 {
		t.Fatal("logout did not clear cookie")
	}
	if _, err := sessions.Resolve(context.Background(), first.Token); err == nil {
		t.Fatal("logout session usable")
	}
	if probe("https://sama.example", first.CSRF).Code != 204 {
		t.Fatal("repeat logout failed")
	}
	if _, err := sessions.Resolve(context.Background(), second.Token); err != nil {
		t.Fatal("other user revoked")
	}
	var audits int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM audit_events WHERE event_type='auth.logout'`).Scan(&audits); err != nil || audits != 1 {
		t.Fatal("logout audit not retained exactly once")
	}
	// Restore a membership for synthetic session issuance, then expire three sessions.
	if _, err := pool.Exec(context.Background(), `INSERT INTO memberships SELECT id,$1,'owner',1 FROM workspaces ORDER BY id LIMIT 1`, user); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		issueSession(t, sessions, pool, user, now, "")
	}
	now = now.Add(31 * time.Minute)
	active := issueSession(t, sessions, pool, user, now, "")
	if count, err := sessions.Cleanup(context.Background(), 1); err != nil || count != 1 {
		t.Fatal("cleanup batch limit")
	}
	if count, err := sessions.Cleanup(context.Background(), 1000); err != nil || count != 2 {
		t.Fatal("cleanup expiry selection")
	}
	if _, err := sessions.Resolve(context.Background(), active.Token); err != nil {
		t.Fatal("cleanup removed active session")
	}
	if _, err := sessions.Cleanup(context.Background(), 1001); err == nil {
		t.Fatal("unbounded cleanup accepted")
	}
}
