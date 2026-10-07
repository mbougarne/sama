//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"sama/backend/internal/connection"
)

func TestConnectionBindingsVersionsAndSafeMetadata(t *testing.T) {
	pool := identityDatabase(t)
	_, first := seedIdentity(t, pool)
	_, second := seedIdentity(t, pool)
	ctx := context.Background()
	a, b := entityID(t), entityID(t)
	create := func(scope, id uuid.UUID) {
		t.Helper()
		_, err := pool.Exec(ctx, `INSERT INTO connections(id,workspace_id,provider,api_family,account_identity,label)
            VALUES($1,$2,'fixture','compute','opaque/account','Test connection')`, id, scope)
		if err != nil {
			t.Fatal(err)
		}
	}
	create(first, a)
	create(second, b)
	credentialSQL := `INSERT INTO credential_versions(workspace_id,connection_id,version,ciphertext,nonce,wrapped_data_key,wrapping_nonce,encryption_format,master_key_id)
        VALUES($1,$2,$3,'ciphertext-sentinel','nonce-sentinel','wrapped-key-sentinel','wrapping-nonce-sentinel',1,'key-sentinel')`
	reject := func(code, sql string, args ...any) {
		t.Helper()
		_, err := pool.Exec(ctx, sql, args...)
		var pgerr *pgconn.PgError
		if !errors.As(err, &pgerr) || pgerr.Code != code {
			t.Fatalf("expected SQLSTATE %s, got %v", code, err)
		}
	}
	reject("23503", credentialSQL, second, a, 1)
	reject("23503", credentialSQL, first, b, 1)
	reject("23514", `UPDATE connections SET status='active' WHERE id=$1`, a)
	reject("23503", `UPDATE connections SET active_credential_version=1 WHERE id=$1`, a)
	for _, field := range []string{"provider", "api_family", "account_identity"} {
		reject("23514", `UPDATE connections SET `+field+`='changed' WHERE id=$1`, a)
	}
	reject("23514", `UPDATE connections SET workspace_id=$1 WHERE id=$2`, second, a)
	for _, version := range []int{1, 2} {
		if _, err := pool.Exec(ctx, credentialSQL, first, a, version); err != nil {
			t.Fatal(err)
		}
	}
	reject("23505", credentialSQL, first, a, 1)
	reject("23514", credentialSQL, first, a, 0)
	reject("23514", `UPDATE credential_versions SET connection_id=$1 WHERE connection_id=$2`, b, a)
	if _, err := pool.Exec(ctx, `UPDATE connections SET status='active',active_credential_version=2 WHERE id=$1`, a); err != nil {
		t.Fatal(err)
	}
	reject("23503", `DELETE FROM credential_versions WHERE connection_id=$1 AND version=2`, a)
	metadata, err := connection.ReadMetadata(ctx, pool, first, a)
	if err != nil || metadata.Status != "active" || metadata.ActiveCredentialVersion == nil || *metadata.ActiveCredentialVersion != 2 {
		t.Fatal("active metadata", metadata, err)
	}
	encoded, err := json.Marshal(metadata)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"ciphertext", "nonce", "wrapped", "key-sentinel"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatal("read metadata leaks credential material")
		}
	}
	if _, err := connection.ReadMetadata(ctx, pool, second, a); err != connection.ErrNotFound {
		t.Fatal("cross-workspace read")
	}
	if _, err := pool.Exec(ctx, `UPDATE connections SET status='disabled' WHERE id=$1`, a); err != nil {
		t.Fatal(err)
	}
	metadata, err = connection.ReadMetadata(ctx, pool, first, a)
	if err != nil || metadata.Status != "disabled" || *metadata.ActiveCredentialVersion != 2 {
		t.Fatal("disable lost retained version")
	}
	if _, err := pool.Exec(ctx, `UPDATE credential_versions SET revoked_at=clock_timestamp() WHERE connection_id=$1 AND version=1`, a); err != nil {
		t.Fatal(err)
	}
	reject("23514", `UPDATE credential_versions SET revoked_at=NULL WHERE connection_id=$1 AND version=1`, a)
	// An active connection and its first version can be installed atomically.
	id := entityID(t)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `INSERT INTO connections(id,workspace_id,provider,api_family,account_identity,label,status,active_credential_version)
        VALUES($1,$2,'fixture','compute','account','Atomic','active',1)`, id, first); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, credentialSQL, first, id, 1); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}
