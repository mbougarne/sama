//go:build integration

package integration

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"golang.org/x/oauth2"
	"sama/backend/internal/httpapi"
	"sama/backend/internal/identity"
)

func TestLoginChallengesOneUseExpiryRateAndRedirect(t *testing.T) {
	pool := identityDatabase(t)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	challenges := identity.NewChallenges(pool, func() time.Time { return now })
	o := &identity.OIDC{Config: identity.OIDCConfig{}, OAuth: oauth2.Config{ClientID: "fixture", RedirectURL: "https://sama.example/auth/callback", Endpoint: oauth2.Endpoint{AuthURL: "https://issuer.example/authorize"}, Scopes: []string{"openid"}}}
	handler := httpapi.NewHandler(&httpapi.Auth{OIDC: o, Challenges: challenges})
	request := httptest.NewRequest("GET", "https://untrusted.example/auth/login", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	location, err := url.Parse(recorder.Header().Get("Location"))
	if err != nil || recorder.Code != 302 || location.Host != "issuer.example" || location.Query().Get("redirect_uri") != "https://sama.example/auth/callback" || location.Query().Get("code_challenge_method") != "S256" {
		t.Fatal("unsafe login redirect")
	}
	cookies := recorder.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != "__Host-sama_login" || !cookies[0].Secure || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteLaxMode || cookies[0].Domain != "" {
		t.Fatal("login cookie boundary")
	}
	state := location.Query().Get("state")
	if _, err := challenges.Consume(context.Background(), state, "wrong"); !errors.Is(err, identity.ErrUnauthenticated) {
		t.Fatal("browser binding ignored")
	}
	c, err := challenges.Consume(context.Background(), state, cookies[0].Value)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256([]byte(c.Verifier))
	if location.Query().Get("code_challenge") != base64.RawURLEncoding.EncodeToString(hash[:]) || c.Nonce != location.Query().Get("nonce") || c.Nonce == c.Verifier || c.Verifier == state {
		t.Fatal("challenge independence/PKCE")
	}
	if _, err := challenges.Consume(context.Background(), state, cookies[0].Value); !errors.Is(err, identity.ErrUnauthenticated) {
		t.Fatal("challenge replay accepted")
	}
	expired, err := challenges.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for range 28 {
		issued, err := challenges.Start(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := challenges.Consume(context.Background(), issued.State, issued.Browser); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := challenges.Start(context.Background()); !errors.Is(err, identity.ErrRateLimited) {
		t.Fatal("consumption bypassed issuance rate")
	}
	now = now.Add(5 * time.Minute)
	if _, err := challenges.Consume(context.Background(), expired.State, expired.Browser); !errors.Is(err, identity.ErrUnauthenticated) {
		t.Fatal("expired state accepted")
	}
	if _, err := challenges.Start(context.Background()); err != nil {
		t.Fatal("rate window did not reset")
	}
	for _, target := range []string{"/auth/login?return_to=https://attacker.example", "/auth/login?issuer=https://attacker.example"} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest("GET", target, nil))
		if recorder.Code != 400 {
			t.Fatal("untrusted login parameters accepted")
		}
	}
}
