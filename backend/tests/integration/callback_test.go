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
	sessions, err := identity.NewSessions(pool, identity.DefaultSessionPolicy(), func() time.Time { return now })
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
			json.NewEncoder(w).Encode(map[string]any{"issuer": server.URL, "authorization_endpoint": server.URL + "/authorize", "token_endpoint": server.URL + "/token", "jwks_uri": server.URL + "/keys"})
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
			if code == "unknown" || strings.HasPrefix(code, "invited") {
				subject = "unknown-identity"
			}
			if code == "bad-nonce" {
				n = "incorrect"
			}
			claims := map[string]any{"iss": server.URL, "sub": subject, "aud": "fixture", "exp": now.Add(time.Hour).Unix(), "iat": now.Unix(), "nonce": n, "email": "same-email@example.invalid"}
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
	o, err := identity.NewOIDC(context.Background(), identity.OIDCConfig{Issuer: server.URL, ClientID: "fixture", PublicOrigin: "http://127.0.0.1:8080", Development: true}, func() time.Time { return now })
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
	var count, audits int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM sessions`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM audit_events WHERE event_type='auth.login'`).Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if count != 2 || audits != 2 {
		t.Fatalf("nonadmitted/failed audit login persisted: sessions=%d audits=%d", count, audits)
	}
}
