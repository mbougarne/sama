//go:build integration

package integration

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

const (
	postgresImage       = "postgres:18"
	postgresPort        = "5432/tcp"
	fixtureStartupLimit = 90 * time.Second
	commandLimit        = 10 * time.Second
)

type postgresFixture struct {
	container string
	port      string
	closed    bool
}

type testDatabase struct {
	name string
	url  string
}

func TestPostgresFixtureIsolatesDatabasesAndCleansUp(t *testing.T) {
	fixture := newPostgresFixture(t)
	first := fixture.createDatabase(t)
	second := fixture.createDatabase(t)
	if first.name == second.name || first.url == second.url {
		t.Fatalf("fixture databases must be distinct: %q and %q", first.name, second.name)
	}
	for _, database := range []testDatabase{first, second} {
		if err := validateExplicitTestConnectionSettings(database); err != nil {
			t.Fatalf("invalid explicit test connection settings for %q: %s", database.name, err)
		}
	}

	fixture.execSQL(t, first.name, "CREATE TABLE fixture_marker (value text NOT NULL); INSERT INTO fixture_marker VALUES ('first database')")
	count := fixture.execSQL(t, second.name, "SELECT count(*) FROM pg_catalog.pg_tables WHERE schemaname = 'public' AND tablename = 'fixture_marker'")
	if count != "0" {
		t.Fatalf("database %q saw first database's marker table: count=%q", second.name, count)
	}

	if err := fixture.close(); err != nil {
		t.Fatalf("remove disposable PostgreSQL container: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), commandLimit)
	defer cancel()
	output, err := docker(ctx, "inspect", fixture.container)
	if err == nil || !strings.Contains(strings.ToLower(string(output)), "no such") {
		t.Fatal("container still exists or cleanup could not be verified")
	}
}

func TestExplicitTestConnectionSettingsErrorDoesNotExposeURL(t *testing.T) {
	const rawURL = "postgres://postgres:redaction-sentinel@127.0.0.1/%zz?password=redaction-sentinel"
	err := validateExplicitTestConnectionSettings(testDatabase{name: "sama_test_redaction", url: rawURL})
	if err == nil {
		t.Fatal("expected malformed test connection URL to fail")
	}
	if strings.Contains(err.Error(), rawURL) || strings.Contains(err.Error(), "redaction-sentinel") {
		t.Fatal("connection URL validation error exposed the raw URL")
	}
}

func TestExplicitTestConnectionSettingsRejectsUnreachableHostPort(t *testing.T) {
	const databaseName = "sama_test_unreachable"
	connection := url.URL{
		Scheme: "postgres",
		User:   url.User("postgres"),
		Host:   net.JoinHostPort("127.0.0.1", "0"),
		Path:   "/" + databaseName,
	}
	query := connection.Query()
	query.Set("sslmode", "disable")
	connection.RawQuery = query.Encode()

	err := validateExplicitTestConnectionSettings(testDatabase{name: databaseName, url: connection.String()})
	if err == nil {
		t.Fatal("unreachable database URL host port was accepted")
	}
}

func validateExplicitTestConnectionSettings(database testDatabase) error {
	parsed, err := url.Parse(database.url)
	if err != nil {
		return errors.New("malformed URL")
	}
	if parsed.Scheme != "postgres" || parsed.Hostname() != "127.0.0.1" || parsed.Query().Get("sslmode") != "disable" || strings.TrimPrefix(parsed.Path, "/") != database.name {
		return errors.New("unexpected URL components")
	}
	if !fixturePortReachable(parsed.Host, commandLimit) {
		return errors.New("database URL host port is unreachable")
	}
	return nil
}

func fixturePortReachable(address string, timeout time.Duration) bool {
	connection, err := net.DialTimeout("tcp", address, timeout)
	if err != nil {
		return false
	}
	_ = connection.Close()
	return true
}

func newPostgresFixture(t *testing.T) *postgresFixture {
	t.Helper()
	if _, err := exec.LookPath("docker"); err != nil {
		t.Fatalf("PostgreSQL integration tests require the Docker CLI and a running daemon: %v", err)
	}
	token, err := randomToken()
	if err != nil {
		t.Fatalf("create disposable fixture identity: %v", err)
	}
	fixture := &postgresFixture{container: "sama-it-postgres-" + token}
	ctx, cancel := context.WithTimeout(context.Background(), fixtureStartupLimit)
	defer cancel()
	output, err := docker(ctx,
		"run", "--detach", "--rm",
		"--name", fixture.container,
		"--label", "sama.integration.fixture=true",
		"--publish", "127.0.0.1::5432",
		"--env", "POSTGRES_HOST_AUTH_METHOD=trust",
		postgresImage,
	)
	t.Cleanup(func() {
		if err := fixture.close(); err != nil {
			t.Errorf("remove disposable PostgreSQL fixture: %v", err)
		}
	})
	if err != nil {
		t.Fatalf("start disposable PostgreSQL 18 fixture (%s): %v: %s", postgresImage, err, strings.TrimSpace(string(output)))
	}

	output, err = docker(ctx, "port", fixture.container, postgresPort)
	if err != nil {
		t.Fatalf("resolve loopback-only PostgreSQL fixture port: %v: %s", err, strings.TrimSpace(string(output)))
	}
	host, portText, err := net.SplitHostPort(strings.TrimSpace(string(output)))
	if err != nil {
		t.Fatalf("parse PostgreSQL fixture port: %v", err)
	}
	if host != "127.0.0.1" {
		t.Fatalf("PostgreSQL fixture port is bound to %q, not loopback", host)
	}
	fixture.port = portText
	port, err := strconv.Atoi(fixture.port)
	if err != nil || port < 1 || port > 65535 {
		t.Fatalf("Docker returned an invalid PostgreSQL fixture port")
	}
	if err := fixture.waitReady(ctx); err != nil {
		t.Fatalf("wait for PostgreSQL 18 fixture: %v", err)
	}
	if err := fixture.waitHostPort(ctx); err != nil {
		t.Fatalf("wait for host-side PostgreSQL fixture port: %s", err)
	}
	version := fixture.execSQL(t, "postgres", "SHOW server_version_num")
	versionNumber, err := strconv.Atoi(version)
	if err != nil || versionNumber < 180000 || versionNumber >= 190000 {
		t.Fatalf("fixture image %s reported PostgreSQL version number %q", postgresImage, version)
	}
	return fixture
}

func (f *postgresFixture) waitHostPort(ctx context.Context) error {
	address := net.JoinHostPort("127.0.0.1", f.port)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		if fixturePortReachable(address, 250*time.Millisecond) {
			return nil
		}
		select {
		case <-ctx.Done():
			return errors.New("timed out waiting for the host-side port")
		case <-ticker.C:
		}
	}
}

func (f *postgresFixture) createDatabase(t *testing.T) testDatabase {
	t.Helper()
	token, err := randomToken()
	if err != nil {
		t.Fatalf("create isolated database identity: %v", err)
	}
	name := "sama_test_" + token
	ctx, cancel := context.WithTimeout(context.Background(), commandLimit)
	defer cancel()
	output, err := docker(ctx, "exec", f.container, "createdb", "--username=postgres", "--owner=postgres", name)
	if err != nil {
		t.Fatalf("create isolated test database: %v: %s", err, strings.TrimSpace(string(output)))
	}
	connection := url.URL{
		Scheme: "postgres",
		User:   url.User("postgres"),
		Host:   net.JoinHostPort("127.0.0.1", f.port),
		Path:   "/" + name,
	}
	query := connection.Query()
	query.Set("sslmode", "disable")
	connection.RawQuery = query.Encode()
	return testDatabase{name: name, url: connection.String()}
}

func (f *postgresFixture) execSQL(t *testing.T, database, statement string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), commandLimit)
	defer cancel()
	output, err := docker(ctx, "exec", f.container, "psql", "--no-psqlrc", "--tuples-only", "--no-align", "--set", "ON_ERROR_STOP=1", "--host=127.0.0.1", "--port=5432", "--username=postgres", "--dbname="+database, "--command", statement)
	if err != nil {
		t.Fatalf("execute fixture SQL: %v: %s", err, strings.TrimSpace(string(output)))
	}
	return strings.TrimSpace(string(output))
}

func (f *postgresFixture) waitReady(ctx context.Context) error {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	var lastError error
	for {
		checkCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		output, err := docker(checkCtx, "exec", f.container, "pg_isready", "--quiet", "--host=127.0.0.1", "--port=5432", "--username=postgres", "--dbname=postgres")
		cancel()
		if err == nil {
			return nil
		}
		lastError = fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
		select {
		case <-ctx.Done():
			return fmt.Errorf("timed out after %s: %w (last readiness check: %v)", fixtureStartupLimit, ctx.Err(), lastError)
		case <-ticker.C:
		}
	}
}

func (f *postgresFixture) close() error {
	if f.closed {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), commandLimit)
	defer cancel()
	output, err := docker(ctx, "inspect", "--type=container", f.container)
	if err != nil {
		if strings.Contains(strings.ToLower(string(output)), "no such") {
			f.closed = true
			return nil
		}
		return errors.New("inspect fixture before cleanup failed")
	}
	output, err = docker(ctx, "rm", "--force", "--volumes", f.container)
	if err != nil {
		return errors.New("remove fixture container failed")
	}
	f.closed = true
	return nil
}

func docker(ctx context.Context, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, "docker", args...)
	return command.CombinedOutput()
}

func randomToken() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(value[:]), nil
}
