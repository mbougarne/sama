//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"sama/backend/internal/connection"
	"sama/backend/internal/platform/credentials"
)

func TestSaveAndRotationUseRequestCredentialAndAtomicAudit(t *testing.T) {
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
	reader.account = "another-account"
	if _, err = service.Rotate(ctx, owner, scope, saved.ID, input.Credential, "rotate"); err != connection.ErrCredential {
		t.Fatal("different account accepted", err)
	}
	reader.account = "safe-account"
	if _, err = pool.Exec(ctx, `ALTER TABLE audit_events ADD CONSTRAINT reject_rotation CHECK(event_type<>'connection.credential_rotated') NOT VALID`); err != nil {
		t.Fatal(err)
	}
	if _, err = service.Rotate(ctx, owner, scope, saved.ID, input.Credential, "rotate"); err == nil {
		t.Fatal("audit failure ignored")
	}
	if pool.QueryRow(ctx, `SELECT count(*) FROM credential_versions WHERE connection_id=$1`, saved.ID).Scan(&count) != nil || count != 1 {
		t.Fatal("rotation rollback lost")
	}
	if _, err = pool.Exec(ctx, `ALTER TABLE audit_events DROP CONSTRAINT reject_rotation`); err != nil {
		t.Fatal(err)
	}
	version, err := service.Rotate(ctx, owner, scope, saved.ID, input.Credential, "rotate")
	if err != nil || version != 2 {
		t.Fatal(version, err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if err = connection.RequireVersion(ctx, tx, scope, saved.ID, 1); err != connection.ErrVersion {
		t.Fatal("old review accepted", err)
	}
}

func TestRewrapBatchesResumeAndPreserveProviderVersions(t *testing.T) {
	pool := identityDatabase(t)
	_, scope := seedIdentity(t, pool)
	ctx := context.Background()
	ring := writeKeyring(t)
	id := entityID(t)
	if _, err := pool.Exec(ctx, `INSERT INTO connections(id,workspace_id,provider,api_family,account_identity,label) VALUES($1,$2,'fixture','compute','account','Fixture')`, id, scope); err != nil {
		t.Fatal(err)
	}
	for version := int64(1); version <= 3; version++ {
		binding := credentials.Binding{Workspace: scope, Connection: id, Family: "compute", Version: version}
		envelope, err := ring.Encrypt(binding, []byte("synthetic"))
		if err != nil {
			t.Fatal(err)
		}
		envelope, err = ring.Rewrap(binding, envelope, "old")
		if err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, `INSERT INTO credential_versions(workspace_id,connection_id,version,ciphertext,nonce,wrapped_data_key,wrapping_nonce,encryption_format,master_key_id) VALUES($1,$2,$3,$4,$5,$6,$7,1,'old')`, scope, id, version, envelope.Ciphertext, envelope.Nonce, envelope.WrappedKey, envelope.WrappingNonce); err != nil {
			t.Fatal(err)
		}
	}
	for _, expected := range []int{2, 1, 0} {
		count, err := connection.RewrapBatch(ctx, pool, ring, "old", "new", 2)
		if err != nil || count != expected {
			t.Fatal(count, err)
		}
	}
	if count, err := connection.KeyReferences(ctx, pool, "old"); err != nil || count != 0 {
		t.Fatal(count, err)
	}
	var count int
	if pool.QueryRow(ctx, `SELECT count(*) FROM credential_versions WHERE connection_id=$1 AND master_key_id='new'`, id).Scan(&count) != nil || count != 3 {
		t.Fatal("versions lost")
	}
}
