//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"testing/fstest"

	"sama/backend/internal/audit"
	"sama/backend/internal/identifier"
	"sama/backend/internal/platform"
	"sama/backend/migrations"
)

const integrationMigrationQueryTimeout = 30 * time.Minute

func TestDatabasePoolBoundsCancellationRollbackAndClose(t *testing.T) {
	fixture := newPostgresFixture(t)
	database := fixture.createDatabase(t)
	pool, err := platform.OpenPool(context.Background(), platform.DatabaseConfig{URL: database.url})
	if err != nil {
		t.Fatal("could not open the disposable PostgreSQL pool")
	}
	if pool.Stat().MaxConns() != 10 {
		t.Fatal("database pool did not use the ten-connection starting limit")
	}

	queryCtx, cancelQuery := context.WithTimeout(context.Background(), 50*time.Millisecond)
	started := time.Now()
	_, err = platform.Exec(queryCtx, pool, "SELECT pg_sleep(5)")
	cancelQuery()
	if err == nil || time.Since(started) > time.Second {
		t.Fatal("cancelled database query was not terminated promptly")
	}

	connections := make([]*pgxpool.Conn, 0, 10)
	for range 10 {
		connection, acquireErr := pool.Acquire(context.Background())
		if acquireErr != nil {
			t.Fatal("could not reserve the configured pool connections")
		}
		connections = append(connections, connection)
	}
	waitCtx, cancelWait := context.WithTimeout(context.Background(), 30*time.Millisecond)
	_, err = platform.Acquire(waitCtx, pool)
	cancelWait()
	if err == nil {
		t.Fatal("acquisition did not stop when its caller was cancelled")
	}
	for _, connection := range connections {
		connection.Release()
	}

	if _, err := platform.Exec(context.Background(), pool, "CREATE TABLE rollback_probe (value integer NOT NULL)"); err != nil {
		t.Fatal("could not create rollback probe")
	}
	tx, err := platform.BeginTx(context.Background(), pool, pgx.TxOptions{})
	if err != nil {
		t.Fatal("could not begin rollback probe transaction")
	}
	if _, err := tx.Exec(context.Background(), "INSERT INTO rollback_probe VALUES (1)"); err != nil {
		t.Fatal("could not write rollback probe")
	}
	if err := tx.Rollback(context.Background()); err != nil {
		t.Fatal("could not roll back probe transaction")
	}
	var count int
	if err := pool.QueryRow(context.Background(), "SELECT count(*) FROM rollback_probe").Scan(&count); err != nil || count != 0 {
		t.Fatal("rolled-back data was visible")
	}

	pool.Close()
	if _, err := platform.Acquire(context.Background(), pool); err == nil {
		t.Fatal("closed database pool accepted a new acquisition")
	}
}

func TestForwardMigrationsAreChecksummedAndAtomic(t *testing.T) {
	fixture := newPostgresFixture(t)
	database := fixture.createDatabase(t)
	pool, err := platform.OpenPool(context.Background(), platform.DatabaseConfig{URL: database.url})
	if err != nil {
		t.Fatal("could not open migration test database")
	}
	defer pool.Close()

	if err := platform.ApplyMigrations(context.Background(), pool, migrations.Files, integrationMigrationQueryTimeout); err != nil {
		t.Fatal("initial forward migrations failed")
	}
	if err := platform.ApplyMigrations(context.Background(), pool, migrations.Files, integrationMigrationQueryTimeout); err != nil {
		t.Fatal("unchanged forward migrations were not idempotent")
	}
	changed := fstest.MapFS{}
	entries, err := fs.ReadDir(migrations.Files, ".")
	if err != nil || len(entries) == 0 {
		t.Fatal("embedded migration files were unavailable")
	}
	for index, entry := range entries {
		contents, readErr := fs.ReadFile(migrations.Files, entry.Name())
		if readErr != nil {
			t.Fatal("embedded migration could not be read")
		}
		if index == 0 {
			contents = append(contents, []byte("\n-- changed after apply\n")...)
		}
		changed[entry.Name()] = &fstest.MapFile{Data: contents, Mode: 0o444}
	}
	if err := platform.ApplyMigrations(context.Background(), pool, changed, integrationMigrationQueryTimeout); err == nil {
		t.Fatal("changed applied migration checksum was accepted")
	}

	failedDatabase := fixture.createDatabase(t)
	failedPool, err := platform.OpenPool(context.Background(), platform.DatabaseConfig{URL: failedDatabase.url})
	if err != nil {
		t.Fatal("could not open atomicity test database")
	}
	defer failedPool.Close()
	failing := fstest.MapFS{
		"000001_marker.sql":  &fstest.MapFile{Data: []byte("CREATE TABLE public.migration_marker (id integer PRIMARY KEY);"), Mode: 0o444},
		"000002_failure.sql": &fstest.MapFile{Data: []byte("CREATE TABLE public.migration_failure ("), Mode: 0o444},
	}
	if err := platform.ApplyMigrations(context.Background(), failedPool, failing, integrationMigrationQueryTimeout); err == nil {
		t.Fatal("invalid migration unexpectedly succeeded")
	}
	var ledger, marker string
	if err := failedPool.QueryRow(context.Background(), "SELECT COALESCE(to_regclass('public.sama_schema_migrations')::text, ''), COALESCE(to_regclass('public.migration_marker')::text, '')").Scan(&ledger, &marker); err != nil {
		t.Fatal("could not verify failed migration rollback")
	}
	if ledger != "" || marker != "" {
		t.Fatal("failed migration left schema or ledger changes behind")
	}

	brokenDatabase := fixture.createDatabase(t)
	brokenPool, err := platform.OpenPool(context.Background(), platform.DatabaseConfig{URL: brokenDatabase.url})
	if err != nil {
		t.Fatal("could not open incompatible-ledger test database")
	}
	defer brokenPool.Close()
	_, err = platform.Exec(context.Background(), brokenPool, "CREATE TABLE public.sama_schema_migrations (version integer PRIMARY KEY)")
	if err != nil {
		t.Fatal("could not create incompatible ledger fixture")
	}
	if err := platform.ApplyMigrations(context.Background(), brokenPool, migrations.Files, integrationMigrationQueryTimeout); err == nil {
		t.Fatal("incompatible migration ledger was accepted")
	}
}

func TestMigrationCanExceedRuntimeQueryTimeout(t *testing.T) {
	fixture := newPostgresFixture(t)
	database := fixture.createDatabase(t)
	pool, err := platform.OpenPool(context.Background(), platform.DatabaseConfig{URL: database.url})
	if err != nil {
		t.Fatal("could not open migration timeout test database")
	}
	defer pool.Close()

	const migrationTimeout = 15 * time.Second
	source := fstest.MapFS{
		"000001_slow_migration.sql": &fstest.MapFile{
			Data: []byte("SELECT pg_sleep(6); CREATE TABLE public.slow_migration_completed (id integer PRIMARY KEY);"),
			Mode: 0o444,
		},
	}
	started := time.Now()
	if err := platform.ApplyMigrations(context.Background(), pool, source, migrationTimeout); err != nil {
		t.Fatal("migration exceeding the runtime query timeout did not complete")
	}
	if time.Since(started) < 5*time.Second {
		t.Fatal("migration timeout probe did not exceed the runtime query bound")
	}
}

func TestDistinctDatabaseRolesAndTransactionalAudit(t *testing.T) {
	fixture := newPostgresFixture(t)
	database := fixture.createDatabase(t)
	adminPool, err := platform.OpenPool(context.Background(), platform.DatabaseConfig{URL: database.url})
	if err != nil {
		t.Fatal("could not open role fixture database")
	}
	defer adminPool.Close()
	if _, err := platform.Exec(context.Background(), adminPool, "CREATE ROLE sama_migrator LOGIN"); err != nil {
		t.Fatal("could not create isolated migration role")
	}
	if _, err := platform.Exec(context.Background(), adminPool, "CREATE ROLE sama_runtime LOGIN"); err != nil {
		t.Fatal("could not create isolated database roles")
	}
	if _, err := platform.Exec(context.Background(), adminPool, "GRANT CONNECT ON DATABASE \""+database.name+"\" TO sama_migrator"); err != nil {
		t.Fatal("could not grant migration database connection")
	}
	if _, err := platform.Exec(context.Background(), adminPool, "GRANT CONNECT ON DATABASE \""+database.name+"\" TO sama_runtime"); err != nil {
		t.Fatal("could not grant runtime database connection")
	}
	if _, err := platform.Exec(context.Background(), adminPool, "GRANT USAGE, CREATE ON SCHEMA public TO sama_migrator"); err != nil {
		t.Fatal("could not prepare migration role privileges")
	}

	migrationURL := roleURL(t, database.url, "sama_migrator")
	migrationPool, err := platform.OpenPool(context.Background(), platform.DatabaseConfig{URL: migrationURL})
	if err != nil {
		t.Fatal("migration identity could not connect")
	}
	defer migrationPool.Close()
	if err := platform.ApplyMigrations(context.Background(), migrationPool, migrations.Files, integrationMigrationQueryTimeout); err != nil {
		t.Fatal("migration identity could not apply the isolated schema")
	}
	probeMigration := make(fstest.MapFS)
	entries, err := fs.ReadDir(migrations.Files, ".")
	if err != nil {
		t.Fatal("embedded migrations were unavailable to migration role test")
	}
	for _, entry := range entries {
		contents, readErr := fs.ReadFile(migrations.Files, entry.Name())
		if readErr != nil {
			t.Fatal("embedded migration could not be read")
		}
		probeMigration[entry.Name()] = &fstest.MapFile{Data: contents, Mode: 0o444}
	}
	probeMigration["000002_probe.sql"] = &fstest.MapFile{Data: []byte("CREATE TABLE public.migration_identity_probe (id integer PRIMARY KEY)"), Mode: 0o444}
	if err := platform.ApplyMigrations(context.Background(), migrationPool, probeMigration, integrationMigrationQueryTimeout); err != nil {
		t.Fatal("migration identity could not apply an isolated fixture migration")
	}

	grantTemplate, err := os.ReadFile(filepath.Join("..", "..", "deploy", "postgres", "grants.sql"))
	if err != nil {
		t.Fatal("database role grant template was unavailable")
	}
	if err := applyGrantTemplate(context.Background(), fixture, database, string(grantTemplate)); err != nil {
		t.Fatal("database role grant template could not be applied")
	}

	runtimeURL := roleURL(t, database.url, "sama_runtime")
	runtimePool, err := platform.OpenPool(context.Background(), platform.DatabaseConfig{URL: runtimeURL})
	if err != nil {
		t.Fatal("runtime identity could not connect")
	}
	defer runtimePool.Close()
	if _, err := platform.Exec(context.Background(), runtimePool, "CREATE TABLE public.runtime_ddl_denied (id integer)"); err == nil {
		t.Fatal("runtime identity performed DDL")
	}

	id, err := identifier.New()
	if err != nil {
		t.Fatal("could not create UUIDv7 identifier")
	}
	var databaseID string
	if err := runtimePool.QueryRow(context.Background(), "SELECT $1::uuid::text", id.String()).Scan(&databaseID); err != nil || databaseID != id.String() {
		t.Fatal("canonical identifier did not round-trip through PostgreSQL")
	}
	encoded, err := json.Marshal(id)
	if err != nil {
		t.Fatal("could not encode public identifier as JSON")
	}
	var jsonID string
	if err := json.Unmarshal(encoded, &jsonID); err != nil || jsonID != id.String() {
		t.Fatal("canonical identifier did not round-trip through JSON")
	}
	if _, err := identifier.Parse(jsonID); err != nil {
		t.Fatal("public identifier parser rejected the round-trip value")
	}

	workspaceID := uuid.MustParse("018f1f4e-7b0b-7cc3-98df-86d5c7d6d802")
	requestID := "request-transaction-1"
	eventID, err := audit.NewID()
	if err != nil {
		t.Fatal("could not create audit identifier")
	}
	event := audit.Event{ID: eventID, Workspace: workspaceID, ActorKind: audit.ActorSystem, Type: "workspace.created", RequestID: requestID}
	tx, err := platform.BeginTx(context.Background(), runtimePool, pgx.TxOptions{})
	if err != nil {
		t.Fatal("runtime identity could not begin audit transaction")
	}
	if err := audit.Append(context.Background(), tx, event); err != nil {
		t.Fatal("runtime identity could not append an audit event")
	}
	if err := tx.Rollback(context.Background()); err != nil {
		t.Fatal("could not roll back audit transaction")
	}
	var count int
	if err := runtimePool.QueryRow(context.Background(), "SELECT count(*) FROM public.audit_events WHERE request_id = $1", requestID).Scan(&count); err != nil || count != 0 {
		t.Fatal("rolled-back caller transaction committed its audit event")
	}

	event.ID, err = audit.NewID()
	if err != nil {
		t.Fatal("could not create committed audit identifier")
	}
	tx, err = platform.BeginTx(context.Background(), runtimePool, pgx.TxOptions{})
	if err != nil {
		t.Fatal("runtime identity could not begin committed audit transaction")
	}
	if err := audit.Append(context.Background(), tx, event); err != nil {
		t.Fatal("runtime identity could not append a committed audit event")
	}
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatal("runtime identity could not commit an audit event")
	}
	for _, statement := range []string{
		"UPDATE public.audit_events SET request_id = 'changed' WHERE request_id = 'request-transaction-1'",
		"DELETE FROM public.audit_events WHERE request_id = 'request-transaction-1'",
	} {
		if _, err := platform.Exec(context.Background(), runtimePool, statement); err == nil {
			t.Fatal("runtime identity modified append-only audit rows")
		}
	}
}

func roleURL(t *testing.T, rawURL, role string) string {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal("could not parse disposable database URL")
	}
	parsed.User = url.User(role)
	return parsed.String()
}

func applyGrantTemplate(ctx context.Context, fixture *postgresFixture, database testDatabase, template string) error {
	commandCtx, cancel := context.WithTimeout(ctx, commandLimit)
	defer cancel()
	command := exec.CommandContext(commandCtx, "docker", "exec", "-i", fixture.container,
		"psql", "--no-psqlrc", "--set", "ON_ERROR_STOP=1",
		"--set", "database_name="+database.name,
		"--set", "migration_role=sama_migrator",
		"--set", "runtime_role=sama_runtime",
		"--host=127.0.0.1", "--port=5432", "--username=postgres", "--dbname="+database.name,
	)
	command.Stdin = strings.NewReader(template)
	if err := command.Run(); err != nil {
		return errors.New("grant template command failed")
	}
	return nil
}
