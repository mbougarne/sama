package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"sama/backend/internal/identity"
	"testing"
)

func TestMutationGuardBindsOriginAndSession(t *testing.T) {
	token := base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	hash := sha256.Sum256([]byte(token))
	auth := &Auth{OIDC: &identity.OIDC{Config: identity.OIDCConfig{PublicOrigin: "https://sama.example"}}}
	handler := withRequestID(auth.requireMutation(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })))
	for _, test := range []struct {
		name, method, origin, media, csrf string
		duplicate                         bool
		want                              int
	}{
		{"valid", "POST", "https://sama.example", "application/json", token, false, 204},
		{"charset", "PATCH", "https://sama.example", "application/json; charset=utf-8", token, false, 204},
		{"missing origin", "DELETE", "", "application/json", token, false, 403},
		{"foreign", "POST", "https://evil.example", "application/json", token, false, 403},
		{"null", "POST", "null", "application/json", token, false, 403},
		{"form", "POST", "https://sama.example", "text/plain", token, false, 403},
		{"missing token", "POST", "https://sama.example", "application/json", "", false, 403},
		{"other session", "POST", "https://sama.example", "application/json", base64.RawURLEncoding.EncodeToString([]byte("another-session-token-32-bytes!!!")), false, 403},
		{"duplicate", "POST", "https://sama.example", "application/json", token, true, 403},
		{"safe read", "GET", "", "", "", false, 204},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := httptest.NewRequest(test.method, "/api/new-route", nil)
			r.Header.Set("Origin", test.origin)
			r.Header.Set("Content-Type", test.media)
			r.Header.Set("X-CSRF-Token", test.csrf)
			r.Header.Set("X-Forwarded-Host", "sama.example")
			if test.duplicate {
				r.Header.Add("Origin", test.origin)
			}
			r = r.WithContext(context.WithValue(r.Context(), principalKey, identity.Principal{CSRFHash: hash[:]}))
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != test.want {
				t.Fatalf("got %d want %d", w.Code, test.want)
			}
			if w.Header().Get("Access-Control-Allow-Origin") != "" {
				t.Fatal("CORS introduced")
			}
		})
	}
}

func TestLogoutWithoutSessionStillRequiresJSON(t *testing.T) {
	auth := &Auth{OIDC: &identity.OIDC{Config: identity.OIDCConfig{PublicOrigin: "https://sama.example"}}}
	r := httptest.NewRequest("POST", "/auth/logout", nil)
	r.Header.Set("Origin", "https://sama.example")
	w := httptest.NewRecorder()
	NewHandler(auth).ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal(w.Code)
	}
}
