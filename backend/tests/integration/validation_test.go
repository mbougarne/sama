//go:build integration

package integration

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sama/backend/internal/httpapi"
	"sama/backend/internal/identity"
	"strings"
	"testing"
	"time"

	"sama/backend/internal/connection"
	"sama/backend/internal/platform/credentials"
	"sama/backend/internal/workspace"
)

type validationReader struct {
	calls   int
	err     error
	account string
}

func (r *validationReader) Validate(_ context.Context, input connection.Credential) (string, []string, error) {
	r.calls++
	if input.Token != "synthetic-secret-sentinel" {
		return "", nil, errors.New("wrong credential")
	}
	return r.account, []string{"compute.server.read"}, r.err
}

func TestValidationPreviewNeverPersistsOrInfersWrites(t *testing.T) {
	pool := identityDatabase(t)
	owner, scope := seedIdentity(t, pool)
	stranger, _ := seedIdentity(t, pool)
	path := filepath.Join(t.TempDir(), "keyring")
	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	if err := os.WriteFile(path, []byte(`{"active":"fixture","keys":[{"id":"fixture","key":"`+key+`"}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	ring, err := credentials.LoadKeyring(path)
	if err != nil {
		t.Fatal(err)
	}
	reader := &validationReader{account: "safe-account"}
	service := connection.NewValidationService(pool, ring, true, map[string]connection.Validator{"fixture/compute": reader})
	input := connection.Credential{Type: "bearer_v1", Token: "synthetic-secret-sentinel"}
	ctx := context.Background()
	if _, err := service.Preview(ctx, stranger, scope, "fixture/compute", input); err != workspace.ErrDenied || reader.calls != 0 {
		t.Fatal("unauthorized provider call", err)
	}
	if _, err := service.Preview(ctx, owner, scope, "unsupported", input); err != connection.ErrUnsupported || reader.calls != 0 {
		t.Fatal("unregistered adapter")
	}
	if _, err := connection.NewValidationService(pool, ring, false, nil).Preview(ctx, owner, scope, "fixture/compute", input); err != connection.ErrValidation {
		t.Fatal("DB-only provider call")
	}
	preview, err := service.Preview(ctx, owner, scope, "fixture/compute", input)
	if err != nil || preview.AccountIdentity != "safe-account" || len(preview.Capabilities) != 1 || !preview.Capabilities[0].Available {
		t.Fatal(preview, err)
	}
	reader.err = errors.New("synthetic-secret-sentinel")
	if _, err := service.Preview(ctx, owner, scope, "fixture/compute", input); err != connection.ErrValidation {
		t.Fatal("raw provider error")
	}
	reader.err = nil
	reader.account = "prefix-synthetic-secret-sentinel"
	if _, err := service.Preview(ctx, owner, scope, "fixture/compute", input); err != connection.ErrValidation {
		t.Fatal("credential echoed as account")
	}
	reader.account = "safe-account"
	for i := 0; i < 2; i++ {
		if _, err := service.Preview(ctx, owner, scope, "fixture/compute", input); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := service.Preview(ctx, owner, scope, "fixture/compute", input); err != connection.ErrValidationLimit {
		t.Fatal("rate budget")
	}
	for _, table := range []string{"connections", "credential_versions", "audit_events"} {
		var count int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM `+table).Scan(&count); err != nil || count != 0 {
			t.Fatal("preview persisted", table, count, err)
		}
	}
}

func TestValidationHTTPSecretsAndStrictInput(t *testing.T) {
	pool := identityDatabase(t)
	owner, scope := seedIdentity(t, pool)
	path := filepath.Join(t.TempDir(), "keyring")
	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	if err := os.WriteFile(path, []byte(`{"active":"fixture","keys":[{"id":"fixture","key":"`+key+`"}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	ring, err := credentials.LoadKeyring(path)
	if err != nil {
		t.Fatal(err)
	}
	reader := &validationReader{account: "safe-account"}
	service := connection.NewValidationService(pool, ring, true, map[string]connection.Validator{"fixture/compute": reader})
	sessions, _ := identity.NewSessions(pool, identity.DefaultSessionPolicy(), time.Now)
	token := issueSession(t, sessions, pool, owner, time.Now().Add(-time.Second), "")
	handler := httpapi.NewHandler(&httpapi.Auth{Pool: pool, Sessions: sessions, ConnectionValidation: service, OIDC: &identity.OIDC{Config: identity.OIDCConfig{PublicOrigin: "https://sama.example"}}})
	request := func(body, csrf string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/api/v1/workspaces/"+scope.String()+"/connection-validations", strings.NewReader(body))
		r.AddCookie(&http.Cookie{Name: "__Host-sama_session", Value: token.Token})
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", "https://sama.example")
		r.Header.Set("X-CSRF-Token", csrf)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if strings.Contains(w.Body.String(), "synthetic-secret-sentinel") {
			t.Fatal("response leaked credential")
		}
		return w
	}
	body := `{"family":"fixture/compute","credential":{"type":"bearer_v1","token":"synthetic-secret-sentinel"}}`
	if request(body, "wrong").Code != 403 || request(strings.Replace(body, `"family"`, `"preview_authority"`, 1), token.CSRF).Code != 400 {
		t.Fatal("mutation boundary")
	}
	if request(strings.Repeat("x", 4097), token.CSRF).Code != 400 {
		t.Fatal("body bound")
	}
	if w := request(body, token.CSRF); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	reader.err = errors.New("synthetic-secret-sentinel")
	if request(body, token.CSRF).Code != 503 {
		t.Fatal("provider failure mapping")
	}
}
