package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const migrationLedgerDDL = `CREATE TABLE IF NOT EXISTS public.sama_schema_migrations (
	version text PRIMARY KEY CONSTRAINT sama_schema_migrations_version_format CHECK (version ~ '^[0-9]{6}$'),
	migration_name text NOT NULL,
	checksum text NOT NULL CONSTRAINT sama_schema_migrations_checksum_format CHECK (checksum ~ '^[0-9a-f]{64}$'),
	applied_at timestamptz NOT NULL DEFAULT clock_timestamp()
)`

var migrationFileName = regexp.MustCompile(`^[0-9]{6}_[a-z0-9_]+\.sql$`)

type migrationFile struct {
	name     string
	version  string
	contents []byte
	checksum string
}

type appliedMigration struct {
	name     string
	checksum string
}

// ApplyMigrations applies immutable SQL files in version order in one
// transaction. queryTimeout bounds migration operations and SQL-file execution
// at both the Go context and PostgreSQL statement level. A checksum mismatch or
// any failed migration leaves the ledger and schema at their pre-invocation state.
func ApplyMigrations(ctx context.Context, pool *pgxpool.Pool, source fs.FS, queryTimeout time.Duration) error {
	if queryTimeout < minMigrationQueryTimeout || queryTimeout > maxMigrationQueryTimeout {
		return errors.New("migration query timeout is invalid")
	}
	files, err := loadMigrationFiles(source)
	if err != nil {
		return errors.New("migration files are invalid")
	}
	connection, err := Acquire(ctx, pool)
	if err != nil {
		return errors.New("migration database is unavailable")
	}
	defer connection.Release()

	beginCtx, cancelBegin := context.WithTimeout(ctx, databaseAcquireTimeout)
	tx, err := connection.BeginTx(beginCtx, pgx.TxOptions{})
	cancelBegin()
	if err != nil {
		return errors.New("migration transaction could not start")
	}
	defer func() {
		rollbackCtx, cancelRollback := context.WithTimeout(context.Background(), databaseQueryTimeout)
		defer cancelRollback()
		_ = tx.Rollback(rollbackCtx)
	}()
	if err := migrationExec(ctx, tx, queryTimeout,
		"SELECT set_config('statement_timeout', $1, true)",
		strconv.FormatInt(queryTimeout.Milliseconds(), 10),
	); err != nil {
		return errors.New("migration query timeout could not be set")
	}

	if err := migrationExec(ctx, tx, queryTimeout, "SELECT pg_advisory_xact_lock($1)", int64(1396789825)); err != nil {
		return errors.New("migration lock could not be acquired")
	}
	if err := migrationExec(ctx, tx, queryTimeout, migrationLedgerDDL); err != nil {
		return errors.New("migration ledger could not be initialized")
	}
	if err := validateMigrationLedger(ctx, tx, queryTimeout); err != nil {
		return errors.New("migration ledger schema is incompatible")
	}
	applied, err := readAppliedMigrations(ctx, tx, queryTimeout)
	if err != nil {
		return errors.New("migration ledger could not be read")
	}
	byVersion := make(map[string]migrationFile, len(files))
	for _, file := range files {
		byVersion[file.version] = file
		if recorded, ok := applied[file.version]; ok &&
			(recorded.checksum != file.checksum || recorded.name != file.name) {
			return errors.New("an applied migration file has changed")
		}
	}
	for version := range applied {
		if _, ok := byVersion[version]; !ok {
			return errors.New("an applied migration file is missing")
		}
	}

	for _, file := range files {
		if _, ok := applied[file.version]; ok {
			continue
		}
		if err := migrationSQL(ctx, tx, queryTimeout, string(file.contents)); err != nil {
			return errors.New("migration SQL failed")
		}
		if err := migrationExec(ctx, tx, queryTimeout,
			"INSERT INTO public.sama_schema_migrations (version, migration_name, checksum) VALUES ($1, $2, $3)",
			file.version, file.name, file.checksum,
		); err != nil {
			return errors.New("migration ledger update failed")
		}
	}
	commitCtx, cancelCommit := context.WithTimeout(ctx, queryTimeout)
	err = tx.Commit(commitCtx)
	cancelCommit()
	if err != nil {
		return errors.New("migration transaction could not commit")
	}
	return nil
}

func loadMigrationFiles(source fs.FS) ([]migrationFile, error) {
	names, err := fs.Glob(source, "*.sql")
	if err != nil {
		return nil, err
	}
	sort.Strings(names)
	files := make([]migrationFile, 0, len(names))
	versions := make(map[string]bool, len(names))
	for _, name := range names {
		if !migrationFileName.MatchString(name) {
			return nil, errors.New("invalid migration filename")
		}
		version := strings.SplitN(name, "_", 2)[0]
		if versions[version] {
			return nil, errors.New("duplicate migration version")
		}
		versions[version] = true
		contents, err := fs.ReadFile(source, name)
		if err != nil {
			return nil, err
		}
		if len(strings.TrimSpace(string(contents))) == 0 {
			return nil, errors.New("empty migration file")
		}
		checksum := sha256.Sum256(contents)
		files = append(files, migrationFile{
			name: name, version: version, contents: contents, checksum: hex.EncodeToString(checksum[:]),
		})
	}
	return files, nil
}

func migrationExec(ctx context.Context, tx pgx.Tx, queryTimeout time.Duration, query string, args ...any) error {
	queryCtx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()
	_, err := tx.Exec(queryCtx, query, args...)
	return err
}

func migrationSQL(ctx context.Context, tx pgx.Tx, queryTimeout time.Duration, sql string) error {
	queryCtx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()
	_, err := tx.Conn().PgConn().Exec(queryCtx, sql).ReadAll()
	return err
}

func validateMigrationLedger(ctx context.Context, tx pgx.Tx, queryTimeout time.Duration) error {
	queryCtx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()
	var compatible bool
	err := tx.QueryRow(queryCtx, `SELECT
		COUNT(*) = 4
		AND COUNT(*) FILTER (WHERE column_name = 'version' AND data_type = 'text' AND is_nullable = 'NO') = 1
		AND COUNT(*) FILTER (WHERE column_name = 'migration_name' AND data_type = 'text' AND is_nullable = 'NO') = 1
		AND COUNT(*) FILTER (WHERE column_name = 'checksum' AND data_type = 'text' AND is_nullable = 'NO') = 1
		AND COUNT(*) FILTER (WHERE column_name = 'applied_at' AND data_type = 'timestamp with time zone' AND is_nullable = 'NO') = 1
		AND EXISTS (
			SELECT 1 FROM information_schema.table_constraints tc
			JOIN information_schema.key_column_usage kcu
			  ON tc.constraint_catalog = kcu.constraint_catalog
			 AND tc.constraint_schema = kcu.constraint_schema
			 AND tc.constraint_name = kcu.constraint_name
			WHERE tc.table_schema = 'public' AND tc.table_name = 'sama_schema_migrations'
			  AND tc.constraint_type = 'PRIMARY KEY' AND kcu.column_name = 'version'
		)
		AND EXISTS (
			SELECT 1 FROM pg_catalog.pg_constraint
			WHERE conrelid = 'public.sama_schema_migrations'::regclass
			  AND conname = 'sama_schema_migrations_version_format' AND contype = 'c'
		)
		AND EXISTS (
			SELECT 1 FROM pg_catalog.pg_constraint
			WHERE conrelid = 'public.sama_schema_migrations'::regclass
			  AND conname = 'sama_schema_migrations_checksum_format' AND contype = 'c'
		)
	FROM information_schema.columns
	WHERE table_schema = 'public' AND table_name = 'sama_schema_migrations'`).Scan(&compatible)
	if err != nil || !compatible {
		return errors.New("incompatible migration ledger")
	}
	return nil
}

func readAppliedMigrations(ctx context.Context, tx pgx.Tx, queryTimeout time.Duration) (map[string]appliedMigration, error) {
	queryCtx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()
	rows, err := tx.Query(queryCtx, "SELECT version, migration_name, checksum FROM public.sama_schema_migrations ORDER BY version")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	applied := make(map[string]appliedMigration)
	for rows.Next() {
		var version string
		var migration appliedMigration
		if err := rows.Scan(&version, &migration.name, &migration.checksum); err != nil {
			return nil, err
		}
		applied[version] = migration
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return applied, nil
}
