# Sama backend

Go owns the browser-facing API and future provider integration. The executable provides HTTP lifecycle handling, JSON logging and a liveness route. It has no identity, PostgreSQL connection, provider adapters or management endpoints yet.

Use the Go toolchain declared in `go.mod`. The baseline is the supported previous Go release series, with its patch version pinned. An installed Go 1.24 command can download and select that toolchain with `GOTOOLCHAIN=auto`; this does not build Sama with Go 1.24 or update the system installation. See [Go toolchain selection](https://go.dev/doc/toolchain).

```sh
go run ./cmd/sama
sh scripts/check.sh
sh scripts/check.sh format
```

Run these from `backend/`. The server binds to `127.0.0.1:8080`; set `SAMA_HTTP_ADDR` to override it. `GET /health` reports process liveness only, not database/provider readiness. Shutdown handles interrupt/termination signals with a bounded drain period. Logs emit fixed diagnostic codes rather than raw errors, configured addresses, or HTTP diagnostic payloads; private context is deliberately omitted. Codes distinguish address conflicts, invalid addresses, permission errors and shutdown timeouts, with generic fallbacks for other failures. The build command produces ignored `bin/sama`.

`check.sh` checks gofmt, runs `go vet`, race-enabled tests and a build. It does not rewrite source. `format` explicitly applies Go formatting; review and stage those changes yourself.

The local module path is `sama/backend` until the real hosting/module identity is selected. No GitHub owner is invented. All future Go tooling, database migrations and backend tests stay in this directory. Create additional modules with their first feature; see the [architecture](../docs/architecture/README.md).
