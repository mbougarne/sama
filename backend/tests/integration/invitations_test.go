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
	"strings"
	"testing"
	"time"
)

func TestInvitationIssuanceCeilingDigestAndAudit(t *testing.T) {
	pool := identityDatabase(t)
	owner, scope := seedIdentity(t, pool)
	admin, _ := seedIdentity(t, pool)
	ctx := context.Background()
	now := time.Now()
	if _, err := pool.Exec(ctx, `INSERT INTO memberships VALUES($1,$2,'admin',1)`, scope, admin); err != nil {
		t.Fatal(err)
	}
	if _, err := workspace.IssueInvitation(ctx, pool, admin, scope, "https://issuer.example", "subject", "owner", "invite-test", now); err != workspace.ErrDenied {
		t.Fatal("admin ceiling")
	}
	sessions, _ := identity.NewSessions(pool, identity.DefaultSessionPolicy(), time.Now)
	credential := issueSession(t, sessions, pool, owner, now.Add(-time.Second), "")
	handler := httpapi.NewHandler(&httpapi.Auth{Pool: pool, Sessions: sessions, OIDC: &identity.OIDC{Config: identity.OIDCConfig{Issuer: "https://issuer.example", PublicOrigin: "https://sama.example"}}})
	issue := func(body, csrf string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/api/v1/workspaces/"+scope.String()+"/invitations", strings.NewReader(body))
		r.AddCookie(&http.Cookie{Name: "__Host-sama_session", Value: credential.Token})
		r.Header.Set("Origin", "https://sama.example")
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", csrf)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	body := `{"subject":"exact-subject","role":"viewer"}`
	if issue(body, "wrong").Code != 403 || issue(`{"subject":"x","role":"owner","issuer":"other"}`, credential.CSRF).Code != 400 {
		t.Fatal("issuance boundary")
	}
	w := issue(body, credential.CSRF)
	var result struct {
		Token string `json:"token"`
	}
	if json.Unmarshal(w.Body.Bytes(), &result) != nil || w.Code != 201 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(w.Body.String())
	}
	digest, ok := workspace.InvitationDigest(result.Token)
	if !ok {
		t.Fatal("proof entropy/encoding")
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM invitations WHERE token_digest=$1 AND issuer='https://issuer.example' AND subject='exact-subject' AND inviter_version=1 AND expires_at>$2`, digest, now.Add(23*time.Hour)).Scan(&count); err != nil || count != 1 {
		t.Fatal("digest/binding/expiry")
	}
	if _, err := pool.Exec(ctx, `ALTER TABLE audit_events ADD CONSTRAINT reject_invite CHECK(event_type<>'invitation.created') NOT VALID`); err != nil {
		t.Fatal(err)
	}
	if issue(body, credential.CSRF).Code != 503 {
		t.Fatal("audit failure not propagated")
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM invitations`).Scan(&count); err != nil || count != 1 {
		t.Fatal("issuance rollback")
	}
	if _, err := pool.Exec(ctx, `ALTER TABLE audit_events DROP CONSTRAINT reject_invite`); err != nil {
		t.Fatal(err)
	}
	for range 99 {
		if _, err := workspace.IssueInvitation(ctx, pool, owner, scope, "https://issuer.example", "exact-subject", "viewer", "invite-limit", now); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := workspace.IssueInvitation(ctx, pool, owner, scope, "https://issuer.example", "exact-subject", "viewer", "invite-limit", now); err != workspace.ErrLimit {
		t.Fatal("unbounded invitations")
	}
	if _, err := workspace.IssueInvitation(ctx, pool, owner, scope, "https://issuer.example", "exact-subject", "viewer", "invite-expiry", now.Add(25*time.Hour)); err != nil {
		t.Fatal("expired proofs not reclaimed")
	}
}
