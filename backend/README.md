# Sama backend

Go owns the browser-facing API and future provider integration. The executable provides HTTP lifecycle handling, JSON logging, bounded optional PostgreSQL connectivity, a liveness route and a shared problem response boundary. It has no identity, provider adapters or management endpoints yet.

Use the Go toolchain declared in `go.mod`. The baseline is the supported previous Go release series, with its patch version pinned. An installed Go 1.24 command can download and select that toolchain with `GOTOOLCHAIN=auto`; this does not build Sama with Go 1.24 or update the system installation. See [Go toolchain selection](https://go.dev/doc/toolchain).

```sh
go run ./cmd/sama
sh scripts/check.sh
sh scripts/check.sh format
```

Run these from `backend/`. Configuration is loaded once at startup. The server binds to `127.0.0.1:8080`; set `SAMA_HTTP_ADDR` to override it. `SAMA_PUBLIC_ORIGIN` accepts an absolute `http` or `https` origin. Set `SAMA_DATABASE_URL` directly or use `SAMA_DATABASE_URL_FILE` for a mounted runtime credential; the direct value wins when both are set. When configured, the server verifies the PostgreSQL connection before listening and opens a pool capped at 10 connections. Pool acquisition is bounded to 2 seconds and PostgreSQL statements to 5 seconds. Neither connection URL nor driver detail is printed in diagnostics. `SAMA_MIGRATION_DATABASE_URL` or `SAMA_MIGRATION_DATABASE_URL_FILE` supplies the separate migration identity to the explicit migration command below. `SAMA_READ_HEADER_TIMEOUT`, `SAMA_READ_TIMEOUT`, `SAMA_WRITE_TIMEOUT`, `SAMA_IDLE_TIMEOUT` and `SAMA_SHUTDOWN_TIMEOUT` accept bounded Go durations. `SAMA_WORKER_COUNT` defaults to 4, `SAMA_PROVIDER_REQUESTS_PER_CONNECTION` defaults to 2, and `SAMA_TRUSTED_PROXY_RANGES` accepts comma-separated CIDR ranges. `GET /health` reports process liveness only, not database/provider readiness. Unknown paths return an `application/problem+json` envelope with a validated, bounded `X-Request-ID`. Shutdown handles interrupt/termination signals with a bounded drain period. Logs emit fixed diagnostic codes rather than raw errors, configured addresses, or HTTP diagnostic payloads; private context is deliberately omitted. Codes distinguish configuration failures, address conflicts, invalid addresses, permission errors and shutdown timeouts, with generic fallbacks for other failures. The build command produces ignored `bin/sama`.

## PostgreSQL migrations and roles

Apply forward migrations explicitly; the HTTP server never migrates its schema:

```sh
SAMA_MIGRATION_DATABASE_URL_FILE=/run/secrets/sama-migration-url go run ./cmd/sama-migrate
```

Provision distinct PostgreSQL migration and runtime login identities for a dedicated Sama database. For the initial migration, grant the migration role database `CONNECT` and `USAGE, CREATE` on schema `public`; apply `deploy/postgres/grants.sql` as a database administrator after the first migration, passing `database_name`, `migration_role` and `runtime_role` as psql variables. The migration role owns the version ledger and migration-created tables. The runtime role receives only schema usage and DML defaults; audit rows are select/insert only, and the schema ledger is inaccessible to it. Later migrations created by the migration identity inherit the runtime DML grants; add table-specific grants when a table needs narrower access.

The migration runner serializes applications, records each numbered SQL file's SHA-256 checksum, rejects missing/changed applied files and incompatible ledger schemas, and applies schema plus ledger updates in one transaction. Migrations are embedded in the binary and must be forward-only. Never edit an applied SQL file; add a higher version instead.

The backend identifier helper creates UUIDv7 entity IDs and accepts only canonical lowercase hyphenated UUID strings at public boundaries. These IDs are not session or CSRF secrets. Audit appends require an existing caller transaction and accept only the event/metadata allowlists; no request or provider body is stored.

`check.sh` checks gofmt, runs `go vet`, race-enabled tests and a build. It does not rewrite source. `format` explicitly applies Go formatting; review and stage those changes yourself.

## Disposable PostgreSQL integration fixture

Run the opt-in integration fixture from `backend/` with:

```sh
go test -tags=integration ./tests/integration
```

The test requires the Docker CLI and a running local Docker daemon. It starts the official `postgres:18` image automatically, binding a dynamically assigned port only on `127.0.0.1`; no existing host database or user data is used. The fixture verifies the server is PostgreSQL 18, creates uniquely named `sama_test_…` databases, and exposes each as a `postgres://postgres@127.0.0.1:<port>/<database>?sslmode=disable` URL for integration tests. The container uses trust authentication only inside this disposable, loopback-bound fixture and contains no production credentials. Docker pulls `postgres:18` if it is not already cached.

The fixture removes its generated container and anonymous data volume through test cleanup, and its integration test verifies removal. Missing Docker or a stopped daemon is a test failure, not a skipped test. Start the local Docker daemon before running the command; no manual database creation or cleanup is needed.

The local module path is `sama/backend` until the real hosting/module identity is selected. No GitHub owner is invented. All Go tooling, database migrations and backend tests stay in this directory. Create additional modules with their first feature; see the [architecture](../docs/architecture/README.md).
