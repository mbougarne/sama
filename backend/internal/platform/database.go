package platform

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	databaseMaxConnections int32 = 10
	databaseAcquireTimeout       = 2 * time.Second
	databaseQueryTimeout         = 5 * time.Second
)

var (
	errDatabaseURLRequired = errors.New("database URL is not configured")
	errDatabaseUnavailable = errors.New("database connection failed")
)

// OpenPool builds and verifies the process database pool. Errors intentionally
// omit driver details because they can contain the configured connection URL.
func OpenPool(ctx context.Context, config DatabaseConfig) (*pgxpool.Pool, error) {
	if config.URL == "" {
		return nil, errDatabaseURLRequired
	}

	poolConfig, err := parsePoolConfig(config)
	if err != nil {
		return nil, errDatabaseUnavailable
	}
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, errDatabaseUnavailable
	}

	connection, err := Acquire(ctx, pool)
	if err != nil {
		pool.Close()
		return nil, errDatabaseUnavailable
	}
	queryCtx, cancel := context.WithTimeout(ctx, databaseQueryTimeout)
	err = connection.Conn().Ping(queryCtx)
	cancel()
	connection.Release()
	if err != nil {
		pool.Close()
		return nil, errDatabaseUnavailable
	}
	return pool, nil
}

func parsePoolConfig(config DatabaseConfig) (*pgxpool.Config, error) {
	parsed, err := pgxpool.ParseConfig(config.URL)
	if err != nil {
		return nil, err
	}
	parsed.MaxConns = databaseMaxConnections
	parsed.ConnConfig.ConnectTimeout = databaseAcquireTimeout
	if parsed.ConnConfig.RuntimeParams == nil {
		parsed.ConnConfig.RuntimeParams = make(map[string]string)
	}
	parsed.ConnConfig.RuntimeParams["statement_timeout"] = "5000"
	parsed.AfterConnect = func(ctx context.Context, connection *pgx.Conn) error {
		_, err := connection.Exec(ctx, "SET statement_timeout = '5s'")
		return err
	}
	return parsed, nil
}

// Acquire applies a finite wait even when the caller has no deadline.
func Acquire(ctx context.Context, pool *pgxpool.Pool) (*pgxpool.Conn, error) {
	acquireCtx, cancel := context.WithTimeout(ctx, databaseAcquireTimeout)
	defer cancel()
	return pool.Acquire(acquireCtx)
}

// Exec applies the common query bound while preserving an earlier caller deadline.
func Exec(ctx context.Context, pool *pgxpool.Pool, query string, args ...any) (pgconn.CommandTag, error) {
	queryCtx, cancel := context.WithTimeout(ctx, databaseQueryTimeout)
	defer cancel()
	return pool.Exec(queryCtx, query, args...)
}

// BeginTx bounds pool acquisition and transaction startup. PostgreSQL's
// statement_timeout bounds each subsequent statement in the pool.
func BeginTx(ctx context.Context, pool *pgxpool.Pool, options pgx.TxOptions) (pgx.Tx, error) {
	acquireCtx, cancel := context.WithTimeout(ctx, databaseAcquireTimeout)
	defer cancel()
	return pool.BeginTx(acquireCtx, options)
}
