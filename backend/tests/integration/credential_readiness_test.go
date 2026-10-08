//go:build integration

package integration

import (
	"context"
	"encoding/base64"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"sama/backend/internal/httpapi"
	"sama/backend/internal/platform/credentials"
)

func TestCredentialReadinessAndLiveness(t *testing.T) {
	pool := identityDatabase(t)
	_, scope := seedIdentity(t, pool)
	id := entityID(t)
	ctx := context.Background()
	for _, sql := range []string{
		`INSERT INTO connections(id,workspace_id,provider,api_family,account_identity,label) VALUES($1,$2,'fixture','compute','account','Fixture')`,
		`INSERT INTO credential_versions(connection_id,workspace_id,version,ciphertext,nonce,wrapped_data_key,wrapping_nonce,encryption_format,master_key_id) VALUES($1,$2,1,'x','n','k','w',1,'retained')`,
	} {
		if _, err := pool.Exec(ctx, sql, id, scope); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(t.TempDir(), "keyring")
	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	if err := os.WriteFile(path, []byte(`{"active":"new","keys":[{"id":"new","key":"`+key+`"}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	ring, err := credentials.LoadKeyring(path)
	if err != nil {
		t.Fatal(err)
	}
	handler := httpapi.CredentialReadiness(httpapi.NewHandler(), pool, ring)
	check := func(path string, want int) {
		t.Helper()
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Header().Get("X-Request-ID") == "" || w.Header().Get("Content-Security-Policy") == "" {
			t.Fatal("readiness response boundary")
		}
		if w.Code != want {
			t.Fatal(path, w.Code)
		}
	}
	check("/readyz", 503)
	check("/health", 200)
	if err := os.WriteFile(path, []byte(`{"active":"new","keys":[{"id":"new","key":"`+key+`"},{"id":"retained","key":"`+key+`"}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	ring, err = credentials.LoadKeyring(path)
	if err != nil {
		t.Fatal(err)
	}
	handler = httpapi.CredentialReadiness(httpapi.NewHandler(), pool, ring)
	check("/readyz", 200)
}
