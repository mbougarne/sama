//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"
	"sama/backend/internal/httpapi"
	"sama/backend/internal/identity"
)

func TestInvitedLoginJSONBindsProofWithoutRedirectingFetch(t *testing.T) {
	challenges := identity.NewChallenges(identityDatabase(t), time.Now)
	oidc := &identity.OIDC{Config: identity.OIDCConfig{PublicOrigin: "https://sama.example"}, OAuth: oauth2.Config{ClientID: "fixture", RedirectURL: "https://sama.example/auth/callback", Endpoint: oauth2.Endpoint{AuthURL: "https://issuer.example/authorize"}}}
	handler := httpapi.NewHandler(&httpapi.Auth{OIDC: oidc, Challenges: challenges})
	proof := strings.Repeat("A", 43)
	for _, origin := range []string{"https://untrusted.example", "https://sama.example"} {
		request := httptest.NewRequest("POST", "/auth/login", strings.NewReader(`{"invitation":"`+proof+`"}`))
		request.Header.Set("Origin", origin)
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Accept", "application/json")
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if origin != "https://sama.example" {
			if recorder.Code != 403 {
				t.Fatal("untrusted origin accepted")
			}
			continue
		}
		var body struct {
			URL string `json:"authorization_url"`
		}
		if recorder.Code != 200 || json.Unmarshal(recorder.Body.Bytes(), &body) != nil {
			t.Fatal("JSON initiation failed")
		}
		destination, err := url.Parse(body.URL)
		if err != nil || destination.Host != "issuer.example" || strings.Contains(body.URL, proof) || destination.Query().Get("code_challenge_method") != "S256" {
			t.Fatal("unsafe authorization URL")
		}
		if recorder.Header().Get("Location") != "" || recorder.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("unsafe fetch response")
		}
		cookies := recorder.Result().Cookies()
		if len(cookies) != 1 || !cookies[0].HttpOnly || !cookies[0].Secure {
			t.Fatal("browser binding cookie missing")
		}
		challenge, err := challenges.Consume(context.Background(), destination.Query().Get("state"), cookies[0].Value)
		if err != nil || len(challenge.InvitationDigest) != 32 {
			t.Fatal("proof not bound to challenge")
		}
	}
}
