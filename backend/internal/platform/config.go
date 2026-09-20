package platform

import (
	"errors"
	"net"
	"net/netip"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultHTTPAddr              = "127.0.0.1:8080"
	defaultPublicOrigin          = "http://127.0.0.1:8080"
	defaultWorkerCount           = 4
	defaultProviderBudget        = 2
	defaultMigrationQueryTimeout = 30 * time.Minute

	minTimeout                       = time.Millisecond
	maxTimeout                       = 10 * time.Minute
	minMigrationQueryTimeout         = time.Second
	maxMigrationQueryTimeout         = 24 * time.Hour
	minWorkers                       = 1
	maxWorkers                       = 32
	minProviderRequestsPerConnection = 1
	maxProviderRequestsPerConnection = 16
)

// Config is the immutable process configuration loaded during startup.
type Config struct {
	HTTPAddr       string
	PublicOrigin   url.URL
	Database       DatabaseConfig
	Timeouts       TimeoutConfig
	Budgets        BudgetConfig
	TrustedProxies []netip.Prefix
}

type DatabaseConfig struct {
	// URL is kept in memory for the database layer. It is never included in
	// configuration errors or diagnostics.
	URL          string
	MigrationURL string
}

type TimeoutConfig struct {
	ReadHeader     time.Duration
	Read           time.Duration
	Write          time.Duration
	Idle           time.Duration
	Shutdown       time.Duration
	MigrationQuery time.Duration
}

type BudgetConfig struct {
	WorkerCount                   int
	ProviderRequestsPerConnection int
}

type EnvLookup func(string) (string, bool)
type FileRead func(string) ([]byte, error)

type ConfigError struct {
	code string
}

func (e *ConfigError) Error() string { return e.code }

func ConfigurationCode(err error) (string, bool) {
	var configErr *ConfigError
	if !errors.As(err, &configErr) || !configurationCodes[configErr.code] {
		return "", false
	}
	return configErr.code, true
}

var configurationCodes = map[string]bool{
	"invalid_http_addr":                      true,
	"invalid_public_origin":                  true,
	"invalid_database_url":                   true,
	"invalid_migration_database_url":         true,
	"database_url_file_unreadable":           true,
	"migration_database_url_file_unreadable": true,
	"invalid_timeout":                        true,
	"invalid_budget":                         true,
	"invalid_trusted_proxy":                  true,
}

func configError(code string) error { return &ConfigError{code: code} }

// Load reads the supported environment variables once for the process.
func Load() (Config, error) {
	return LoadFrom(os.LookupEnv, os.ReadFile)
}

func LoadFrom(lookup EnvLookup, readFile FileRead) (Config, error) {
	address := defaultHTTPAddr
	if value, ok := lookup("SAMA_HTTP_ADDR"); ok {
		address = value
	}
	if !validListenAddress(address) {
		return Config{}, configError("invalid_http_addr")
	}

	originText := defaultPublicOrigin
	if value, ok := lookup("SAMA_PUBLIC_ORIGIN"); ok {
		originText = value
	}
	origin, err := parseOrigin(originText)
	if err != nil {
		return Config{}, configError("invalid_public_origin")
	}

	databaseURL, err := databaseURL(lookup, readFile)
	if err != nil {
		return Config{}, err
	}
	migrationDatabaseURL, err := migrationDatabaseURL(lookup, readFile)
	if err != nil {
		return Config{}, err
	}

	timeouts, err := loadTimeouts(lookup)
	if err != nil {
		return Config{}, err
	}
	budgets, err := loadBudgets(lookup)
	if err != nil {
		return Config{}, err
	}
	proxies, err := loadTrustedProxies(lookup)
	if err != nil {
		return Config{}, err
	}

	return Config{
		HTTPAddr:       address,
		PublicOrigin:   origin,
		Database:       DatabaseConfig{URL: databaseURL, MigrationURL: migrationDatabaseURL},
		Timeouts:       timeouts,
		Budgets:        budgets,
		TrustedProxies: proxies,
	}, nil
}

func databaseURL(lookup EnvLookup, readFile FileRead) (string, error) {
	return databaseURLSetting(lookup, readFile,
		"SAMA_DATABASE_URL", "SAMA_DATABASE_URL_FILE",
		"invalid_database_url", "database_url_file_unreadable")
}

func migrationDatabaseURL(lookup EnvLookup, readFile FileRead) (string, error) {
	return databaseURLSetting(lookup, readFile,
		"SAMA_MIGRATION_DATABASE_URL", "SAMA_MIGRATION_DATABASE_URL_FILE",
		"invalid_migration_database_url", "migration_database_url_file_unreadable")
}

func databaseURLSetting(lookup EnvLookup, readFile FileRead, directKey, fileKey, invalidCode, fileErrorCode string) (string, error) {
	if value, ok := lookup(directKey); ok {
		if !validDatabaseURL(value) {
			return "", configError(invalidCode)
		}
		return strings.TrimSpace(value), nil
	}
	path, ok := lookup(fileKey)
	if !ok {
		return "", nil
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return "", configError(fileErrorCode)
	}
	contents, err := readFile(path)
	if err != nil {
		return "", configError(fileErrorCode)
	}
	value := strings.TrimSpace(string(contents))
	if !validDatabaseURL(value) {
		return "", configError(invalidCode)
	}
	return value, nil
}

func validDatabaseURL(value string) bool {
	value = strings.TrimSpace(value)
	u, err := url.Parse(value)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") || u.Host == "" {
		return false
	}
	if u.Port() != "" {
		port, err := strconv.Atoi(u.Port())
		if err != nil || port < 1 || port > 65535 {
			return false
		}
	}
	return true
}

func parseOrigin(value string) (url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(value))
	if err != nil || u.Opaque != "" || u.User != nil || u.Host == "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return url.URL{}, errors.New("invalid origin")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return url.URL{}, errors.New("invalid origin")
	}
	if u.Path != "" && u.Path != "/" {
		return url.URL{}, errors.New("invalid origin")
	}
	if u.Hostname() == "" {
		return url.URL{}, errors.New("invalid origin")
	}
	if u.Port() != "" {
		port, err := strconv.Atoi(u.Port())
		if err != nil || port < 1 || port > 65535 {
			return url.URL{}, errors.New("invalid origin")
		}
	}
	u.Path = ""
	return *u, nil
}

func validListenAddress(value string) bool {
	host, portText, err := net.SplitHostPort(value)
	if err != nil || portText == "" || strings.Contains(portText, ":") {
		return false
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return false
	}
	return host == "" || net.ParseIP(host) != nil || validHostname(host)
}

func validHostname(value string) bool {
	if strings.ContainsAny(value, " /\\*?") || strings.HasPrefix(value, ".") || strings.HasSuffix(value, ".") {
		return false
	}
	return true
}

func loadTimeouts(lookup EnvLookup) (TimeoutConfig, error) {
	readHeader, err := durationSetting(lookup, "SAMA_READ_HEADER_TIMEOUT", 5*time.Second)
	if err != nil {
		return TimeoutConfig{}, err
	}
	read, err := durationSetting(lookup, "SAMA_READ_TIMEOUT", 10*time.Second)
	if err != nil {
		return TimeoutConfig{}, err
	}
	write, err := durationSetting(lookup, "SAMA_WRITE_TIMEOUT", 10*time.Second)
	if err != nil {
		return TimeoutConfig{}, err
	}
	idle, err := durationSetting(lookup, "SAMA_IDLE_TIMEOUT", 60*time.Second)
	if err != nil {
		return TimeoutConfig{}, err
	}
	shutdown, err := durationSetting(lookup, "SAMA_SHUTDOWN_TIMEOUT", 10*time.Second)
	if err != nil {
		return TimeoutConfig{}, err
	}
	migrationQuery, err := boundedDurationSetting(
		lookup, "SAMA_MIGRATION_QUERY_TIMEOUT", defaultMigrationQueryTimeout,
		minMigrationQueryTimeout, maxMigrationQueryTimeout,
	)
	if err != nil {
		return TimeoutConfig{}, err
	}
	return TimeoutConfig{
		ReadHeader: readHeader, Read: read, Write: write, Idle: idle,
		Shutdown: shutdown, MigrationQuery: migrationQuery,
	}, nil
}

func durationSetting(lookup EnvLookup, name string, fallback time.Duration) (time.Duration, error) {
	return boundedDurationSetting(lookup, name, fallback, minTimeout, maxTimeout)
}

func boundedDurationSetting(lookup EnvLookup, name string, fallback, minimum, maximum time.Duration) (time.Duration, error) {
	value, ok := lookup(name)
	if !ok {
		return fallback, nil
	}
	duration, err := time.ParseDuration(strings.TrimSpace(value))
	if err != nil || duration < minimum || duration > maximum {
		return 0, configError("invalid_timeout")
	}
	return duration, nil
}

func loadBudgets(lookup EnvLookup) (BudgetConfig, error) {
	workers, err := integerSetting(lookup, "SAMA_WORKER_COUNT", defaultWorkerCount, minWorkers, maxWorkers)
	if err != nil {
		return BudgetConfig{}, err
	}
	providerRequests, err := integerSetting(lookup, "SAMA_PROVIDER_REQUESTS_PER_CONNECTION", defaultProviderBudget, minProviderRequestsPerConnection, maxProviderRequestsPerConnection)
	if err != nil {
		return BudgetConfig{}, err
	}
	return BudgetConfig{WorkerCount: workers, ProviderRequestsPerConnection: providerRequests}, nil
}

func integerSetting(lookup EnvLookup, name string, fallback, minimum, maximum int) (int, error) {
	value, ok := lookup(name)
	if !ok {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || parsed < minimum || parsed > maximum {
		return 0, configError("invalid_budget")
	}
	return parsed, nil
}

func loadTrustedProxies(lookup EnvLookup) ([]netip.Prefix, error) {
	value, ok := lookup("SAMA_TRUSTED_PROXY_RANGES")
	if !ok || strings.TrimSpace(value) == "" {
		return nil, nil
	}
	parts := strings.Split(value, ",")
	proxies := make([]netip.Prefix, 0, len(parts))
	for _, part := range parts {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(part))
		if err != nil || !prefix.IsValid() {
			return nil, configError("invalid_trusted_proxy")
		}
		if prefix != prefix.Masked() {
			return nil, configError("invalid_trusted_proxy")
		}
		proxies = append(proxies, prefix)
	}
	return proxies, nil
}
