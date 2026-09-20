package platform

import (
	"strings"
	"testing"
)

func TestParsePoolConfigBoundsConnectionsAndStatements(t *testing.T) {
	config, err := parsePoolConfig(DatabaseConfig{URL: "postgres://user:secret@localhost/sama"})
	if err != nil {
		t.Fatal("valid pool configuration was rejected")
	}
	if config.MaxConns != 10 || config.ConnConfig.ConnectTimeout != databaseAcquireTimeout {
		t.Fatalf("unexpected connection limits: max=%d timeout=%s", config.MaxConns, config.ConnConfig.ConnectTimeout)
	}
	if config.ConnConfig.RuntimeParams["statement_timeout"] != "5000" {
		t.Fatal("database statements do not have the configured server-side bound")
	}
	if config.AfterConnect == nil {
		t.Fatal("pool did not enforce the server-side statement bound after connecting")
	}
}

func TestOpenPoolErrorsDoNotExposeDatabaseURL(t *testing.T) {
	const connectionURL = "postgres://sama:secret-sentinel@127.0.0.1:1/sama?sslmode=disable"
	_, err := OpenPool(t.Context(), DatabaseConfig{URL: connectionURL})
	if err == nil || strings.Contains(err.Error(), "secret-sentinel") || strings.Contains(err.Error(), connectionURL) {
		t.Fatal("database connection failure was absent or exposed private connection details")
	}
}
