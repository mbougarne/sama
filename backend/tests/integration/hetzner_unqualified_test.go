//go:build integration

package integration

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"sama/backend/internal/connection"
	"sama/backend/internal/httpapi"
	"sama/backend/internal/identity"
	"sama/backend/internal/platform/credentials"
)

func TestUnqualifiedHetznerCannotPreviewSaveOrInferProjectIdentity(t *testing.T) {
	pool := identityDatabase(t)
	actor, scope := seedIdentity(t, pool)
	path := filepath.Join(t.TempDir(), "keyring")
	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	if os.WriteFile(path, []byte(`{"active":"fixture","keys":[{"id":"fixture","key":"`+key+`"}]}`), 0600) != nil {
		t.Fatal("fixture keyring")
	}
	ring, err := credentials.LoadKeyring(path)
	if err != nil {
		t.Fatal(err)
	}
	// Mirror production's closed registry; no provider client or ambient identity
	// exists in this path. A token prefix/hash is never accepted as project truth.
	service := connection.NewValidationService(pool, ring, true, nil)
	input := connection.Credential{Type: "bearer_v1", Token: "synthetic-hetzner-token"}
	if _, err = service.Preview(context.Background(), actor, scope, "hetzner.cloud", input); err != connection.ErrUnsupported {
		t.Fatal("unqualified preview enabled", err)
	}
	if _, err = service.Save(context.Background(), actor, scope, connection.SaveInput{Family: "hetzner.cloud", Label: "Unqualified", Credential: input}, "unsupported-test"); err != connection.ErrUnsupported {
		t.Fatal("unqualified save enabled", err)
	}
	sessions, _ := identity.NewSessions(pool, identity.DefaultSessionPolicy(), time.Now)
	token := issueSession(t, sessions, pool, actor, time.Now().Add(-time.Second), "")
	handler := httpapi.NewHandler(&httpapi.Auth{Pool: pool, Sessions: sessions, ConnectionValidation: service, OIDC: &identity.OIDC{Config: identity.OIDCConfig{PublicOrigin: "https://sama.example"}}})
	r := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/"+scope.String()+"/connection-validations", strings.NewReader(`{"family":"hetzner.cloud","credential":{"type":"bearer_v1","token":"synthetic-hetzner-token"}}`))
	r.AddCookie(&http.Cookie{Name: "__Host-sama_session", Value: token.Token})
	r.Header.Set("Origin", "https://sama.example")
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-CSRF-Token", token.CSRF)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 422 || !strings.Contains(w.Body.String(), "unsupported_capability") || strings.Contains(w.Body.String(), input.Token) || strings.Contains(w.Body.String(), "account_identity") {
		t.Fatal("unsafe unsupported response", w.Code)
	}
	for _, table := range []string{"connections", "credential_versions", "jobs", "sync_runs", "audit_events"} {
		var count int
		if pool.QueryRow(context.Background(), `SELECT count(*) FROM `+table).Scan(&count) != nil || count != 0 {
			t.Fatal("unqualified credential persisted", table)
		}
	}
}
