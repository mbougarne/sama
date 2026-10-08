package connection

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"sama/backend/internal/platform"
	"sama/backend/internal/platform/credentials"
)

var ErrRewrap = errors.New("credential rewrap unavailable")

// RewrapBatch commits at most limit retained versions independently. Calling it
// again resumes from rows still referencing oldID, including revoked versions.
func RewrapBatch(ctx context.Context, pool *pgxpool.Pool, ring *credentials.Keyring, oldID, newID string, limit int) (int, error) {
	if oldID == newID || limit < 1 || limit > 200 || ring.Require([]string{oldID, newID}) != nil {
		return 0, ErrRewrap
	}
	tx, err := platform.BeginTx(ctx, pool, pgx.TxOptions{})
	if err != nil {
		return 0, ErrRewrap
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `SELECT v.workspace_id,v.connection_id,c.api_family,v.version,v.ciphertext,v.nonce,
 v.wrapped_data_key,v.wrapping_nonce,v.master_key_id,v.encryption_format
 FROM credential_versions v JOIN connections c ON (c.workspace_id,c.id)=(v.workspace_id,v.connection_id)
 WHERE v.master_key_id=$1 ORDER BY v.workspace_id,v.connection_id,v.version LIMIT $2 FOR UPDATE OF v`, oldID, limit)
	if err != nil {
		return 0, ErrRewrap
	}
	type item struct {
		binding  credentials.Binding
		envelope credentials.Envelope
	}
	pending := []item{}
	for rows.Next() {
		var value item
		err = rows.Scan(&value.binding.Workspace, &value.binding.Connection, &value.binding.Family, &value.binding.Version,
			&value.envelope.Ciphertext, &value.envelope.Nonce, &value.envelope.WrappedKey, &value.envelope.WrappingNonce, &value.envelope.KeyID, &value.envelope.Format)
		if err != nil {
			rows.Close()
			return 0, ErrRewrap
		}
		pending = append(pending, value)
	}
	rows.Close()
	if rows.Err() != nil {
		return 0, ErrRewrap
	}
	for _, value := range pending {
		next, err := ring.Rewrap(value.binding, value.envelope, newID)
		if err != nil {
			return 0, ErrRewrap
		}
		result, err := tx.Exec(ctx, `UPDATE credential_versions SET wrapped_data_key=$4,wrapping_nonce=$5,master_key_id=$6
  WHERE workspace_id=$1 AND connection_id=$2 AND version=$3 AND master_key_id=$7`, value.binding.Workspace, value.binding.Connection, value.binding.Version, next.WrappedKey, next.WrappingNonce, newID, oldID)
		if err != nil || result.RowsAffected() != 1 {
			return 0, ErrRewrap
		}
	}
	if tx.Commit(ctx) != nil {
		return 0, ErrRewrap
	}
	return len(pending), nil
}

// KeyReferences counts every retained version, not only active credentials.
// A zero count says nothing about historical backups or other installations.
func KeyReferences(ctx context.Context, pool *pgxpool.Pool, id string) (int64, error) {
	var count int64
	if pool.QueryRow(ctx, `SELECT count(*) FROM credential_versions WHERE master_key_id=$1`, id).Scan(&count) != nil {
		return 0, ErrRewrap
	}
	return count, nil
}
