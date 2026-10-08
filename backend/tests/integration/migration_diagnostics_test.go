//go:build integration

package integration

import (
	"context"
	"errors"
	"io/fs"
	"sort"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"sama/backend/migrations"
)

// Replay public, tracked schema SQL in a rolled-back fixture transaction only.
// Never include database URLs, SQL contents or PostgreSQL detail fields.
func diagnoseMigrationFailure(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		return
	}
	defer tx.Rollback(ctx)
	names, err := fs.Glob(migrations.Files, "*.sql")
	if err != nil {
		return
	}
	sort.Strings(names)
	for _, name := range names {
		sql, err := fs.ReadFile(migrations.Files, name)
		if err != nil {
			return
		}
		_, err = tx.Conn().PgConn().Exec(ctx, string(sql)).ReadAll()
		if err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) {
				t.Logf("migration %s: SQLSTATE %s; schema message: %s", name, pgErr.Code, pgErr.Message)
			} else {
				t.Logf("migration %s failed without a PostgreSQL diagnostic", name)
			}
			return
		}
	}
}
