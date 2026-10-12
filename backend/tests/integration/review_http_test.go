//go:build integration

package integration

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"sama/backend/internal/httpapi"
	"sama/backend/internal/identity"
	"sama/backend/internal/operation"
)

func operationHTTP(t *testing.T, pool *pgxpool.Pool, actor uuid.UUID, s *operation.Service) func(string, string, string, bool) *httptest.ResponseRecorder {
	t.Helper()
	sessions, _ := identity.NewSessions(pool, identity.DefaultSessionPolicy(), time.Now)
	token := issueSession(t, sessions, pool, actor, time.Now().Add(-time.Second), "")
	handler := httpapi.NewHandler(&httpapi.Auth{Pool: pool, Sessions: sessions, Operations: s, OIDC: &identity.OIDC{Config: identity.OIDCConfig{PublicOrigin: "https://sama.example"}}})
	return func(path, body, key string, csrf bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		r.AddCookie(&http.Cookie{Name: "__Host-sama_session", Value: token.Token})
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", "https://sama.example")
		if csrf {
			r.Header.Set("X-CSRF-Token", token.CSRF)
		}
		if key != "" {
			r.Header.Set("Idempotency-Key", key)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
}

func TestReviewHTTPRequiresCSRFAndRejectsEditableMetadata(t *testing.T) {
	s, pool, lease, actor, target, _, _ := acceptanceSetup(t)
	request := operationHTTP(t, pool, actor, s)
	path := "/api/v1/workspaces/" + lease.Workspace.String() + "/resources/" + target.String() + "/action-reviews"
	body := `{"action":"compute.server.power_on","parameters":{}}`
	if request(path, body, "", false).Code != 403 {
		t.Fatal("missing CSRF accepted")
	}
	for _, input := range []string{`{"action":"compute.server.power_on","parameters":{"token":"synthetic-secret"}}`, `{"action":"compute.server.power_on","parameters":{},"policy_version":1}`} {
		if w := request(path, input, "", true); w.Code != 400 || strings.Contains(w.Body.String(), "synthetic-secret") {
			t.Fatal("browser metadata accepted/leaked", w.Code)
		}
	}
	w := request(path, body, "", true)
	var review operation.Review
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &review) != nil || review.ResourceID != target || review.TargetName != "Safe" {
		t.Fatal("actual review response", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "intent_hash") || strings.Contains(w.Body.String(), "credential_version") {
		t.Fatal("internal review facts exposed")
	}
}
