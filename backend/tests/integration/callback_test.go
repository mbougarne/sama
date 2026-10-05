//go:build integration

package integration

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"sama/backend/internal/httpapi"
	"sama/backend/internal/identity"
	"sama/backend/internal/workspace"
)

func TestOIDCCallbackAdmissionBindingAndAudit(t *testing.T) {
	pool := identityDatabase(t)
	user, scope := seedIdentity(t, pool)
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	challenges := identity.NewChallenges(pool, func() time.Time { return now })
	sessions, err := identity.NewSessions(pool, identity.DefaultSessionPolicy(), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	nonce, verifier := "", ""
	failAudit := false
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			json.NewEncoder(w).Encode(map[string]any{"issuer": server.URL, "authorization_endpoint": server.URL + "/authorize", "token_endpoint": server.URL + "/token", "jwks_uri": server.URL + "/keys", "claims_supported": []string{"auth_time"}, "acr_values_supported": []string{"fixture-mfa"}})
		case "/keys":
			json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: "fixture", Algorithm: "RS256", Use: "sig"}}})
		case "/token":
			if err := r.ParseForm(); err != nil {
				t.Error(err)
				return
			}
			code := r.Form.Get("code")
			if code == "bad-code" || r.Form.Get("code_verifier") != verifier {
				w.WriteHeader(400)
				json.NewEncoder(w).Encode(map[string]string{"error": "invalid_grant"})
				return
			}
			subject, n := user.String(), nonce
			if code == "unknown" || strings.HasPrefix(code, "invited") || code == "reauth-switch" {
				subject = "unknown-identity"
			}
			if code == "bad-nonce" {
				n = "incorrect"
			}
			claims := map[string]any{"iss": server.URL, "sub": subject, "aud": "fixture", "exp": now.Add(time.Hour).Unix(), "iat": now.Unix(), "nonce": n, "email": "same-email@example.invalid"}
			if strings.HasPrefix(code, "reauth") {
				claims["auth_time"] = time.Now().Unix()
				claims["acr"] = "fixture-mfa"
				if code == "reauth-stale" {
					claims["auth_time"] = now.Add(-5 * time.Minute).Unix()
				}
				if code == "reauth-policy" {
					claims["acr"] = "wrong"
				}
				if code == "reauth-missing" {
					delete(claims, "auth_time")
				}
			}
			signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key}, nil)
			if err != nil {
				t.Error(err)
				return
			}
			body, _ := json.Marshal(claims)
			signed, err := signer.Sign(body)
			if err != nil {
				t.Error(err)
				return
			}
			token, _ := signed.CompactSerialize()
			json.NewEncoder(w).Encode(map[string]any{"id_token": token, "access_token": "synthetic-access", "token_type": "Bearer"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	if _, err := pool.Exec(context.Background(), `UPDATE users SET issuer=$1 WHERE id=$2`, server.URL, user); err != nil {
		t.Fatal(err)
	}
	o, err := identity.NewOIDC(context.Background(), identity.OIDCConfig{Issuer: server.URL, ClientID: "fixture", PublicOrigin: "http://127.0.0.1:8080", Development: true, ReauthACR: "fixture-mfa"}, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	// Token transport uses a synthetic loopback issuer; exercise production cookies.
	o.Config.Development = false
	handler := httpapi.NewHandler(&httpapi.Auth{OIDC: o, Challenges: challenges, Sessions: sessions, Pool: pool})
	proof, err := workspace.IssueInvitation(context.Background(), pool, user, scope, server.URL, "unknown-identity", "viewer", "invite", now)
	if err != nil {
		t.Fatal(err)
	}
	for _, code := range []string{"bad-code", "bad-nonce", "unknown", "good", "invite-mismatch", "invited-good", "invited-replay", "audit-failure"} {
		t.Run(code, func(t *testing.T) {
			c, err := challenges.Start(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if strings.HasPrefix(code, "invite") {
				request := httptest.NewRequest("POST", "/auth/login", strings.NewReader(`{"invitation":"`+proof+`"}`))
				request.Header.Set("Origin", o.Config.PublicOrigin)
				request.Header.Set("Content-Type", "application/json")
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, request)
				if response.Code != 302 {
					t.Fatal("invited initiation", response.Body.String())
				}
				location, _ := url.Parse(response.Header().Get("Location"))
				c.State = location.Query().Get("state")
				for _, cookie := range response.Result().Cookies() {
					if cookie.Name == "__Host-sama_login" {
						c.Browser = cookie.Value
					}
				}
				digest, _ := workspace.InvitationDigest(c.State)
				if err := pool.QueryRow(context.Background(), `SELECT nonce,verifier FROM login_challenges WHERE state_digest=$1`, digest).Scan(&c.Nonce, &c.Verifier); err != nil {
					t.Fatal(err)
				}
			}
			nonce, verifier = c.Nonce, c.Verifier
			failAudit = code == "audit-failure"
			if failAudit {
				if _, err := pool.Exec(context.Background(), `ALTER TABLE audit_events ADD CONSTRAINT reject_login CHECK(event_type<>'auth.login') NOT VALID`); err != nil {
					t.Fatal(err)
				}
			}
			target := "/auth/callback?" + url.Values{"state": {c.State}, "code": {code}}.Encode()
			request := httptest.NewRequest("GET", target, nil)
			request.AddCookie(&http.Cookie{Name: "__Host-sama_login", Value: c.Browser})
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			want := 401
			if code == "good" || code == "invited-good" {
				want = 302
			}
			if failAudit {
				want = 503
			}
			if recorder.Code != want {
				t.Fatalf("status %d want %d", recorder.Code, want)
			}
			if strings.Contains(recorder.Body.String(), "synthetic-access") {
				t.Fatal("OIDC token leaked")
			}
			if code == "good" || code == "invited-good" {
				var session *http.Cookie
				for _, cookie := range recorder.Result().Cookies() {
					if cookie.Name == "__Host-sama_session" {
						session = cookie
					}
				}
				if session == nil || !session.Secure || !session.HttpOnly || session.Path != "/" || session.Domain != "" || session.SameSite != http.SameSiteLaxMode {
					t.Fatal("production session cookie")
				}
				if _, err := sessions.Resolve(context.Background(), session.Value); err != nil {
					t.Fatal("session not persisted")
				}
			}
			replay := httptest.NewRecorder()
			handler.ServeHTTP(replay, request)
			if replay.Code != 401 {
				t.Fatal("callback replay accepted")
			}
		})
	}
	if _, err := pool.Exec(context.Background(), `ALTER TABLE audit_events DROP CONSTRAINT reject_login`); err != nil {
		t.Fatal(err)
	}
	for _, code := range []string{"reauth-stale", "reauth-policy", "reauth-missing", "reauth-switch", "reauth-revoked", "reauth-good"} {
		t.Run(code, func(t *testing.T) {
			old := issueSession(t, sessions, pool, user, now, "")
			request := httptest.NewRequest("POST", "/auth/reauthenticate", strings.NewReader(`{}`))
			request.AddCookie(&http.Cookie{Name: "__Host-sama_session", Value: old.Token})
			request.Header.Set("Origin", o.Config.PublicOrigin)
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("X-CSRF-Token", old.CSRF)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != 302 {
				t.Fatal("reauth initiation", response.Body.String())
			}
			location, _ := url.Parse(response.Header().Get("Location"))
			query := location.Query()
			if query.Get("prompt") != "login" || query.Get("max_age") != "0" || query.Get("acr_values") != "fixture-mfa" {
				t.Fatal("reauthentication policy parameters")
			}
			var browser string
			for _, cookie := range response.Result().Cookies() {
				if cookie.Name == "__Host-sama_login" {
					browser = cookie.Value
				}
			}
			digest, _ := workspace.InvitationDigest(query.Get("state"))
			if err := pool.QueryRow(context.Background(), `SELECT nonce,verifier FROM login_challenges WHERE state_digest=$1`, digest).Scan(&nonce, &verifier); err != nil {
				t.Fatal(err)
			}
			if code == "reauth-revoked" {
				if err := sessions.Revoke(context.Background(), old.Token); err != nil {
					t.Fatal(err)
				}
			}
			callback := httptest.NewRequest("GET", "/auth/callback?"+url.Values{"state": {query.Get("state")}, "code": {code}}.Encode(), nil)
			callback.AddCookie(&http.Cookie{Name: "__Host-sama_login", Value: browser})
			callback.AddCookie(&http.Cookie{Name: "__Host-sama_session", Value: old.Token})
			result := httptest.NewRecorder()
			handler.ServeHTTP(result, callback)
			want := 403
			if code == "reauth-good" {
				want = 302
			}
			if code == "reauth-switch" || code == "reauth-revoked" {
				want = 401
			}
			if result.Code != want {
				t.Fatalf("reauth status %d want %d: %s", result.Code, want, result.Body.String())
			}
			if code == "reauth-good" {
				if _, err := sessions.Resolve(context.Background(), old.Token); err != identity.ErrUnauthenticated {
					t.Fatal("old session survived")
				}
				for _, cookie := range result.Result().Cookies() {
					if cookie.Name == "__Host-sama_session" {
						p, err := sessions.Resolve(context.Background(), cookie.Value)
						if err != nil || sessions.RequireRecent(p) != nil {
							t.Fatal("rotated session not recent", err)
						}
						if _, err := workspace.ResolveMembership(context.Background(), pool, p.UserID, scope, "owner"); err != nil {
							t.Fatal("workspace authority lost")
						}
					}
				}
			}
			replay := httptest.NewRecorder()
			handler.ServeHTTP(replay, callback)
			if replay.Code != 401 {
				t.Fatal("reauth replay accepted")
			}
		})
	}

	var count, audits int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM sessions`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM audit_events WHERE event_type='auth.login'`).Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if count != 7 || audits != 3 {
		t.Fatalf("nonadmitted/failed audit login persisted: sessions=%d audits=%d", count, audits)
	}
}
