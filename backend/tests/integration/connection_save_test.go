//go:build integration

package integration

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"sama/backend/internal/connection"
	"sama/backend/internal/platform/credentials"
)

func writeKeyring(t *testing.T) *credentials.Keyring {
	t.Helper()
	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	path := filepath.Join(t.TempDir(), "keyring")
	if err := os.WriteFile(path, []byte(`{"active":"new","keys":[{"id":"old","key":"`+key+`"},{"id":"new","key":"`+key+`"}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	ring, err := credentials.LoadKeyring(path)
	if err != nil {
		t.Fatal(err)
	}
	return ring
}

func TestSaveUsesRequestCredentialAndAtomicIntent(t *testing.T) {
	pool := identityDatabase(t)
	owner, scope := seedIdentity(t, pool)
	ctx := context.Background()
	ring := writeKeyring(t)
	reader := &validationReader{account: "safe-account"}
	service := connection.NewValidationService(pool, ring, true, map[string]connection.Validator{"fixture/compute": reader})
	input := connection.SaveInput{Family: "fixture/compute", Label: "Fixture", Credential: connection.Credential{Type: "bearer_v1", Token: "synthetic-secret-sentinel"}}
	reader.err = errors.New("provider unavailable")
	if _, err := service.Save(ctx, owner, scope, input, "save"); err != connection.ErrValidation {
		t.Fatal(err)
	}
	var count int
	for _, table := range []string{"connections", "credential_versions", "audit_events", "sync_runs", "jobs"} {
		if pool.QueryRow(ctx, `SELECT count(*) FROM `+table).Scan(&count) != nil || count != 0 {
			t.Fatal("failed validation persisted", table)
		}
	}
	reader.err = nil
	saved, err := service.Save(ctx, owner, scope, input, "save")
	if err != nil {
		t.Fatal(err)
	}
	var envelope credentials.Envelope
	if err = pool.QueryRow(ctx, `SELECT ciphertext,nonce,wrapped_data_key,wrapping_nonce,master_key_id,encryption_format FROM credential_versions WHERE connection_id=$1 AND version=1`, saved.ID).Scan(&envelope.Ciphertext, &envelope.Nonce, &envelope.WrappedKey, &envelope.WrappingNonce, &envelope.KeyID, &envelope.Format); err != nil {
		t.Fatal(err)
	}
	plain, err := ring.Decrypt(credentials.Binding{Workspace: scope, Connection: saved.ID, Family: input.Family, Version: 1}, envelope)
	if err != nil {
		t.Fatal(err)
	}
	var decrypted connection.Credential
	if json.Unmarshal(plain, &decrypted) != nil || decrypted != input.Credential {
		t.Fatal("stored a different credential")
	}
	if pool.QueryRow(ctx, `SELECT count(*) FROM jobs WHERE sync_id=$1 AND payload::text LIKE '%synthetic-secret%'`, saved.SyncID).Scan(&count) != nil || count != 0 {
		t.Fatal("secret in queue")
	}
}
