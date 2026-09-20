package platform

import (
	"errors"
	"net/netip"
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	config, err := LoadFrom(emptyEnv, unexpectedFileRead)
	if err != nil {
		t.Fatal(err)
	}
	if config.HTTPAddr != "127.0.0.1:8080" || config.PublicOrigin.String() != "http://127.0.0.1:8080" {
		t.Fatalf("unexpected defaults: %+v", config)
	}
	if config.Database.URL != "" || config.Budgets.WorkerCount != 4 || config.Budgets.ProviderRequestsPerConnection != 2 {
		t.Fatalf("unexpected database or budget defaults: %+v", config)
	}
	if config.Timeouts.Read != 10*time.Second || config.Timeouts.Shutdown != 10*time.Second ||
		config.Timeouts.MigrationQuery != defaultMigrationQueryTimeout || len(config.TrustedProxies) != 0 {
		t.Fatalf("unexpected timeout or proxy defaults: %+v", config)
	}
}

func TestDatabaseURLFileAndDirectPrecedence(t *testing.T) {
	values := map[string]string{"SAMA_DATABASE_URL_FILE": "/run/secrets/database-url"}
	config, err := LoadFrom(mapEnv(values), func(path string) ([]byte, error) {
		if path != "/run/secrets/database-url" {
			t.Fatalf("unexpected file path %q", path)
		}
		return []byte("postgres://file-user:file-secret@db/sama\n"), nil
	})
	if err != nil || config.Database.URL != "postgres://file-user:file-secret@db/sama" {
		t.Fatal("file-backed database URL was not loaded")
	}

	values["SAMA_DATABASE_URL"] = "postgres://direct-user:direct-secret@db/sama"
	config, err = LoadFrom(mapEnv(values), func(string) ([]byte, error) {
		t.Fatal("database URL file should not be read when direct value is set")
		return nil, nil
	})
	if err != nil || config.Database.URL != values["SAMA_DATABASE_URL"] {
		t.Fatal("direct database URL did not take precedence")
	}
}

func TestMigrationDatabaseURLIsSeparateAndFileBacked(t *testing.T) {
	values := map[string]string{"SAMA_MIGRATION_DATABASE_URL_FILE": "/run/secrets/migration-url"}
	config, err := LoadFrom(mapEnv(values), func(path string) ([]byte, error) {
		if path != "/run/secrets/migration-url" {
			t.Fatal("unexpected secret file requested")
		}
		return []byte("postgres://migrator:secret@db/sama\n"), nil
	})
	if err != nil || config.Database.URL != "" || config.Database.MigrationURL != "postgres://migrator:secret@db/sama" {
		t.Fatal("migration connection settings were not loaded separately")
	}

	values["SAMA_MIGRATION_DATABASE_URL"] = "postgres://migrator:secret@migration-db/sama"
	config, err = LoadFrom(mapEnv(values), func(string) ([]byte, error) {
		t.Fatal("migration URL file should not be read when direct value is set")
		return nil, nil
	})
	if err != nil || config.Database.MigrationURL != values["SAMA_MIGRATION_DATABASE_URL"] {
		t.Fatal("direct migration URL did not take precedence")
	}
}

func TestInvalidConfigurationHasAllowlistedCodeAndNoSecret(t *testing.T) {
	for _, tc := range []struct {
		name   string
		values map[string]string
		code   string
	}{
		{"origin", map[string]string{"SAMA_PUBLIC_ORIGIN": "not-an-origin"}, "invalid_public_origin"},
		{"origin query delimiter", map[string]string{"SAMA_PUBLIC_ORIGIN": "https://console.example.test?"}, "invalid_public_origin"},
		{"budget", map[string]string{"SAMA_WORKER_COUNT": "0"}, "invalid_budget"},
		{"proxy", map[string]string{"SAMA_TRUSTED_PROXY_RANGES": "10.0.0.0/8,broken"}, "invalid_trusted_proxy"},
		{"timeout", map[string]string{"SAMA_READ_TIMEOUT": "-1s"}, "invalid_timeout"},
		{"migration query timeout too short", map[string]string{"SAMA_MIGRATION_QUERY_TIMEOUT": "500ms"}, "invalid_timeout"},
		{"migration query timeout too long", map[string]string{"SAMA_MIGRATION_QUERY_TIMEOUT": "24h1m"}, "invalid_timeout"},
		{"database URL", map[string]string{"SAMA_DATABASE_URL": "postgres://user:password@"}, "invalid_database_url"},
		{"migration database URL", map[string]string{"SAMA_MIGRATION_DATABASE_URL": "postgres://user:password@"}, "invalid_migration_database_url"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := LoadFrom(mapEnv(tc.values), unexpectedFileRead)
			code, ok := ConfigurationCode(err)
			if !ok || code != tc.code || err.Error() == "" {
				t.Fatalf("code = %q, ok = %t, err = %v", code, ok, err)
			}
			if err.Error() == "password" || err.Error() == "not-an-origin" {
				t.Fatalf("configuration error leaked rejected content: %q", err)
			}
		})
	}
}

func TestLoadParsesTypedSettings(t *testing.T) {
	values := map[string]string{
		"SAMA_HTTP_ADDR":                        "127.0.0.1:9090",
		"SAMA_PUBLIC_ORIGIN":                    "https://console.example.test:8443/",
		"SAMA_READ_TIMEOUT":                     "3s",
		"SAMA_SHUTDOWN_TIMEOUT":                 "45s",
		"SAMA_MIGRATION_QUERY_TIMEOUT":          "17m",
		"SAMA_WORKER_COUNT":                     "8",
		"SAMA_PROVIDER_REQUESTS_PER_CONNECTION": "4",
		"SAMA_TRUSTED_PROXY_RANGES":             "10.0.0.0/8, 2001:db8::/32",
	}
	config, err := LoadFrom(mapEnv(values), unexpectedFileRead)
	if err != nil {
		t.Fatal(err)
	}
	if config.HTTPAddr != values["SAMA_HTTP_ADDR"] || config.PublicOrigin.String() != "https://console.example.test:8443" {
		t.Fatalf("unexpected address/origin: %+v", config)
	}
	if config.Timeouts.Read != 3*time.Second || config.Timeouts.Shutdown != 45*time.Second ||
		config.Timeouts.MigrationQuery != 17*time.Minute || config.Budgets.WorkerCount != 8 ||
		config.Budgets.ProviderRequestsPerConnection != 4 {
		t.Fatalf("unexpected typed settings: %+v", config)
	}
	wantProxies := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8"), netip.MustParsePrefix("2001:db8::/32")}
	if len(config.TrustedProxies) != len(wantProxies) || config.TrustedProxies[0] != wantProxies[0] || config.TrustedProxies[1] != wantProxies[1] {
		t.Fatalf("trusted proxies = %v, want %v", config.TrustedProxies, wantProxies)
	}
}

func TestDatabaseFileReadFailureIsSafe(t *testing.T) {
	_, err := LoadFrom(mapEnv(map[string]string{"SAMA_DATABASE_URL_FILE": "/run/secrets/database-url"}), func(string) ([]byte, error) {
		return nil, errors.New("secret file details")
	})
	code, ok := ConfigurationCode(err)
	if !ok || code != "database_url_file_unreadable" || err.Error() != code {
		t.Fatalf("unsafe file error: code=%q ok=%t err=%v", code, ok, err)
	}
}

func emptyEnv(string) (string, bool) { return "", false }

func unexpectedFileRead(string) ([]byte, error) {
	return nil, errors.New("unexpected file read")
}

func mapEnv(values map[string]string) EnvLookup {
	return func(name string) (string, bool) {
		value, ok := values[name]
		return value, ok
	}
}
