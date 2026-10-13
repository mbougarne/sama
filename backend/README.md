# Sama backend

Go owns the browser-facing API and future provider integration. The executable provides HTTP lifecycle handling, JSON logging, bounded optional PostgreSQL connectivity, a liveness route and a shared problem response boundary. OIDC identity, opaque sessions, explicit owner bootstrap and workspace access are implemented below. Provider adapters and cloud-management endpoints remain future work.

Use the Go toolchain declared in `go.mod`. The baseline is the supported previous Go release series, with its patch version pinned. An installed Go 1.24 command can download and select that toolchain with `GOTOOLCHAIN=auto`; this does not build Sama with Go 1.24 or update the system installation. See [Go toolchain selection](https://go.dev/doc/toolchain).

```sh
go run ./cmd/sama
sh scripts/check.sh
sh scripts/check.sh format
```

Run these from `backend/`. Configuration is loaded once at startup. The server binds to `127.0.0.1:8080`; set `SAMA_HTTP_ADDR` to override it. `SAMA_PUBLIC_ORIGIN` accepts an absolute `http` or `https` origin. Set `SAMA_DATABASE_URL` directly or use `SAMA_DATABASE_URL_FILE` for a mounted runtime credential; the direct value wins when both are set. When configured, the server verifies the PostgreSQL connection before listening and opens a pool capped at 10 connections. Pool acquisition is bounded to 2 seconds and runtime PostgreSQL statements to 5 seconds. Neither connection URL nor driver detail is printed in diagnostics. `SAMA_MIGRATION_DATABASE_URL` or `SAMA_MIGRATION_DATABASE_URL_FILE` supplies the separate migration identity to the explicit migration command below. That command uses a separate per-query/migration-file timeout, defaulting to 30 minutes; set `SAMA_MIGRATION_QUERY_TIMEOUT` to a Go duration from 1 second through 24 hours to tune it. `SAMA_READ_HEADER_TIMEOUT`, `SAMA_READ_TIMEOUT`, `SAMA_WRITE_TIMEOUT`, `SAMA_IDLE_TIMEOUT` and `SAMA_SHUTDOWN_TIMEOUT` accept bounded Go durations. `SAMA_WORKER_COUNT` defaults to 4, `SAMA_PROVIDER_REQUESTS_PER_CONNECTION` defaults to 2, and `SAMA_TRUSTED_PROXY_RANGES` accepts comma-separated CIDR ranges. `GET /health` reports process liveness only, not database/provider readiness. Unknown paths return an `application/problem+json` envelope with a validated, bounded `X-Request-ID`. Shutdown handles interrupt/termination signals with a bounded drain period. Logs emit fixed diagnostic codes rather than raw errors, configured addresses, or HTTP diagnostic payloads; private context is deliberately omitted. Codes distinguish configuration failures, address conflicts, invalid addresses, permission errors and shutdown timeouts, with generic fallbacks for other failures. The build command produces ignored `bin/sama`.

## OIDC configuration

Set `SAMA_OIDC_ISSUER` and `SAMA_OIDC_CLIENT_ID` together. An optional
`SAMA_OIDC_CLIENT_SECRET_FILE` supplies a mounted client secret. Startup verifies
the configured discovery document; HTTP requests cannot choose another issuer.
Production issuer and public origin require HTTPS. The callback is always
`SAMA_PUBLIC_ORIGIN` plus `/auth/callback`, independent of request Host headers.
Discovery and token/key HTTP requests have a five-second timeout, a 1 MiB response
limit and no automatic redirects. Tokens require RS256/ES256 signatures, the
configured issuer/client audience and unexpired claims.

For a synthetic local issuer only, `SAMA_OIDC_DEVELOPMENT=true` explicitly permits
HTTP and requires loopback issuer and public origin. Keep real credentials out of
this mode. OIDC requires a configured database with migrations applied.
`GET /auth/login` redirects to the configured issuer using independent state,
nonce and S256 PKCE values. A browser-bound challenge expires after five minutes
and can be consumed only once. Installation-wide initiation is limited to 30 per
minute and 1000 stored challenges; arbitrary redirect/issuer parameters are
rejected. `GET /auth/callback` consumes state before exchanging the code, verifies
nonce and token claims, and admits only an existing issuer/subject identity with
an active workspace membership. Email matching never enrolls a user. Session and
allowlisted login audit commit together; production cookies use
`__Host-sama_session`, Secure, HttpOnly, SameSite=Lax, Path=/ and no Domain.

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

## Explicit installation owner

After migrations and runtime grants, run the local command using the configured
issuer and exact OIDC subject (email is not an identity key):

```sh
go run ./cmd/sama-bootstrap --issuer https://issuer.example --subject exact-subject --workspace-name 'My workspace'
```

Use the runtime database setting and the same OIDC configuration as the server.
The command creates the initial identity, workspace, owner membership and audit
atomically. It refuses any repeat or an installation already containing a
workspace. It prints no identity or credential details and exposes no web route.

Authenticated `/api/` requests resolve the session against PostgreSQL on every
request. Tenant paths additionally resolve current membership and role; revoked
membership returns 404 on the next request, while an insufficient role in a
known workspace returns 403. Missing/expired sessions return 401. UI role hints
are never authoritative. No domain endpoint is implied by this middleware.

`POST /auth/logout` requires the configured Origin, JSON content type and the
session-bound `X-CSRF-Token`. Login supplies a separate readable same-origin
`__Host-sama_csrf` cookie (development: `sama_csrf`) for this header. Logout
atomically revokes the session and appends audit, expires both cookies, and is
safe to repeat. Audit retains the session's workspace scope after membership
removal. `go run ./cmd/sama-sessions-cleanup` explicitly removes at most 1000
expired sessions per invocation using the runtime database identity.

`GET /api/v1/me` exposes only the current user's UUID and display name.
`GET /api/v1/workspaces` lists current active memberships as `{data,next_cursor}`,
including UUID, name and role. Its default page size is 20 (maximum 100); pass the
returned UUID cursor for the next stable page. Neither response exposes issuer
subjects, session/CSRF digests, OIDC tokens or credentials.

`POST /api/v1/workspaces` accepts JSON `{name}` from an admitted authenticated
user with configured Origin and session-bound CSRF. Names contain 1–100 Unicode
characters, no control characters or outer whitespace; unknown fields and bodies
above 1 KiB are rejected. Creation commits the workspace, creator owner membership
and audit together. A per-user row lock enforces at most ten owned workspaces,
including concurrent requests. Invalid names return 422 and the limit returns 429.

`GET /api/v1/workspaces/{workspace_id}/members` lists safe membership metadata
in user UUID order (default 50, maximum 200). Owners see all roles; admins see
viewer/operator members within their management ceiling. Other roles are denied.
Cursors select a position only and never expand workspace access.

`PUT /api/v1/workspaces/{workspace_id}/members/{user_id}` accepts `{role,version}`;
version 0 assigns an existing admitted identity. `DELETE` accepts `{version}`.
Both require Origin/CSRF, recheck current authority under a workspace lock, reject
stale versions/final-owner removal with 409, and commit audit atomically.
Admins can manage only viewer/operator members; only owners assign higher roles.

`POST /api/v1/workspaces/{workspace_id}/ownership-transfers` requires an owner,
Origin/CSRF and `{user_id,actor_version,target_version,demote}`. The target must
already be a current member. Optional `demote: true` makes the caller an admin;
the grant, demotion and both affected-member audit events commit together.

`POST /api/v1/workspaces/{workspace_id}/invitations` accepts `{subject,role}`
with Origin/CSRF. The issuer comes from installation configuration. Owners/admins
may invite within their current ceiling; at most 100 unexpired invitations exist
per workspace. The response contains a random one-use `token`, shown only once
for manual sharing, valid 24 hours. Only its digest is stored; no email is sent.

To accept an invitation, `POST /auth/login` with JSON `{invitation}` and the
configured Origin. Proof stays out of URLs and is bound to the one-use browser
challenge. This preauthentication initiation creates no membership/session.
Verified callback must match the exact issuer/subject; it rechecks the inviter's
current membership version and ceiling, then consumes proof, creates admission,
session and audit atomically. Expired/replayed/mismatched proof fails closed.
Existing members use explicit membership changes; invitations do not overwrite roles.

For high-impact reauthentication, explicitly set `SAMA_OIDC_REAUTH_ACR` to the
reviewed issuer authentication policy (including MFA where required). Discovery
must advertise that ACR and `auth_time`; otherwise reauthentication fails closed.
The reusable five-minute gate accepts only a verified reauthentication round trip,
never a fresh token `iat` alone. Sessions predating this feature require reauthentication.

`POST /auth/reauthenticate` accepts `{}` with Origin/CSRF and an active session.
It requests `prompt=login`, `max_age=0` and the configured ACR. Callback verifies
signed `auth_time` is no earlier than initiation (whole-second precision), less
than five minutes old and not in the future, with exact ACR equality. The original
session must remain active, browser-bound and owned by the same verified identity.
Successful reauthentication rotates session/CSRF with login audit; workspace roles
remain server-authoritative. An unsupported policy returns an explanatory 403.
See [OIDC authentication requests](https://openid.net/specs/openid-connect-core-1_0.html#AuthRequest).
Membership grants and role elevation revoke the affected user's existing sessions;
their next login issues a new credential before newly granted authority is used.

Owners can `GET` and `PUT /api/v1/workspaces/{workspace_id}/settings`.
PUT requires Origin/CSRF and all fields: `name`, current `policy_version`,
`queue_limit` (1–1000, default 1000), and `audit_retention_days` (180–3650,
default 180). Changed settings increment policy version and audit atomically;
a no-op preserves the version, and a stale version returns 409. The version is
shared with membership policy changes. These stored limits are policy inputs for
future queue/retention workers; this endpoint does not launch workers or delete
evidence. Process-wide installation budgets and membership/grant authority remain
independent, and unresolved-operation/idempotency retention floors still apply.

All authenticated mutation routes inherit the same Origin/JSON/session-bound
CSRF boundary, including newly added routes. Duplicate Origin, Content-Type or
CSRF headers fail closed. GET/HEAD/OPTIONS cannot be used for domain mutations.
OIDC login initiation is the preauthentication exception: invitation initiation
still requires the configured Origin and JSON, then binds proof into the one-use
browser challenge; callback remains protected by state, nonce and PKCE.
Even an already-logged-out logout request requires Origin and JSON.

Inbound mutation bodies have a global 1 MiB cap before authentication or database
work; individual endpoints retain stricter decoding limits and reject unknown
fields. Parsed headers are capped at 32 KiB, and the HTTP server also bounds header
reads and slow clients using its configured timeouts. Oversized bodies/headers
return 413/431; cancelled body reads return a safe 408 when a response is possible.
Only configured trusted proxy peers can supply an X-Forwarded-For client-IP hint;
the chain is checked from right to left. Forwarded host/protocol values never alter
Origin, callback or cookie policy, and forwarding headers are removed downstream.

Logout accepts an empty body or an empty JSON object; unknown fields and trailing
JSON values are rejected even when no session remains.

All HTTP responses carry a strict same-origin CSP: no inline/eval/third-party
scripts or styles, objects, frames, framing ancestors, or base changes. Other
headers deny framing, MIME sniffing, referrer disclosure, camera, microphone and
geolocation. TLS/HSTS termination remains deployment-owned. Production Webpack
builds verify external hashed scripts and extracted CSS against this policy.

Set `SAMA_ASSET_DIR` to the independently built `frontend/dist` directory to serve
the production backoffice from Go. An unset setting keeps API-only behavior;
startup fails safely if a configured directory lacks a regular `index.html`.
Build the frontend first; Go never invokes Node or copies source into its build.
Client routes fall back to the entry document, while API/auth misses stay JSON
problems. Files are confined through `os.Root`, with no directory listings,
hidden files or arbitrary text-file serving. Missing assets return 404.
The entry document uses `no-store`; hashed JS/CSS use one-year immutable caching;
other allowed assets revalidate. Serve only trusted production build output here.

Invitation login with `Accept: application/json` returns `{authorization_url}`
instead of redirecting the fetch request. The URL comes from the configured OIDC
issuer, contains no invitation proof, and retains the one-use browser challenge.
Other login requests retain their redirect response.

Migration 000014 stores workspace-scoped connections and credential versions.
Provider/API-family/account identity is immutable. Active pointers use a deferred
composite foreign key so connection and first version can commit atomically;
disabled connections can retain their active pointer for later reconciliation.
The connection read projection selects metadata only. This storage foundation
adds no connection HTTP routes, encryption service, provider calls or dispatch;
later services must enforce grants and reject revoked versions before use.

Connection grants are deny-by-default, including for owners. `GET` and `PUT`
`/api/v1/workspaces/{workspace_id}/connections/{connection_id}/grants` read the
caller's grant or replace a member's `{user_id,actions}` grant. Classes are `read`,
`refresh`, `operate`, `high_impact`; nonempty grants require `read`. Owners may
explicitly grant themselves. Admins can manage only viewer/operator grants within
their own connection authority. Current role/status and grants gate each call.

`SAMA_KEYRING_FILE` selects a regular mode-0600 JSON file with `active` key ID and
`keys: [{id,key}]`, where each key is base64-encoded 32-byte material. Provision
keys outside Sama; no replacement keys are generated. `GET /readyz` checks the
mounted keyring against all retained credential key references; `/health` stays
live if keys are missing. Restart to load a changed file. Encryption binds both
AES-GCM envelopes to workspace, connection, provider/API family and version.
Only the versioned bearer credential schema is defined; no provider is enabled.
Compiled outbound profiles enforce HTTPS/path/authority, reject redirects and
pin dialing to public DNS answers while preserving TLS hostname verification.

Provider metadata requests have an 8 MiB cap and one 30-second retry budget.
Only GET/HEAD retry, at most five attempts; 401/403 stop and mutation methods
receive one attempt. Provider errors expose fixed categories only. A shared
process budget holds at most two requests per connection through body close,
with credential/project-wide 429 cooldown. Adapters normalize reset timestamps.
`SAMA_PROVIDER_CALLING_PROCESSES` accepts only 0 (DB-only API process) or 1
(default). Deployment must run at most one calling process; local configuration
cannot discover a misconfigured second host. Provider-facing services must reject
calls in DB-only mode and share the same budget instance.

`POST /api/v1/workspaces/{workspace_id}/connection-validations` accepts only
`{family,credential:{type:"bearer_v1",token}}`, requires admin/owner authority,
Origin/CSRF, and returns safe account/read-capability preview metadata. Five
adapter attempts per actor/minute are permitted; a preview writes no connection,
credential or audit and cannot authorize a later save. No production adapters
are registered yet, so configured deployments reject unsupported families.
`POST /api/v1/workspaces/{id}/connections` accepts only family, label and a
write-only typed credential. It freshly validates that request outside its
transaction, then commits the encrypted version, server-derived account and
read qualification, allowlisted audit event and initial sync intent together.
Initial admission uses current management authority and creates no grants.
No production validation adapter is registered until qualification is complete.

`GET /api/v1/workspaces/{workspace_id}/connections` returns granted safe metadata
with a UUID cursor, default limit 50 and maximum 200. Detail and `/capabilities`
reads require the same current membership and explicit read grant. Reasons retain
unsupported, unverified, denied and state-restricted distinctions. No resource
mutation state is inferred from connection-level metadata.

`POST /api/v1/workspaces/{workspace_id}/connections/{connection_id}/disable`
accepts `{}` from an administrator/owner with Origin/CSRF. It atomically disables
new work, cancels queued connection-refresh records and appends audit once.
Credentials/history remain stored. This does not revoke credentials at the
provider. Future sync acceptance and unsent dispatch must retain the shared
`RequireNewWork` row lock through their database transaction; submitted-operation
reconciliation is a separate future service and must remain visible.

The durable jobs migration stores version-1 `connection_refresh` jobs with a bounded JSON
connection reference, tenant foreign key, deadline and lease fields/indexes.
Unknown payload versions, extra fields and raw provider bodies are rejected.
A new payload version plus migration is required before adding sync/operation
references; old versions must stay readable until queued work is drained or
explicitly migrated. Provider execution is implemented in later worker slices. Version 2 adds a composite-FK sync reference without
reinterpreting version 1. Refresh acceptance now coalesces pending/running global
compute-server scopes across replicas and enforces the workspace queue limit.
`POST .../connections/{id}/syncs` requires current explicit read and refresh
grants and an active usable credential; it returns 202 with the sync reference.
Claims use short SKIP LOCKED transactions, 60-second token-fenced leases and
15-second renewal. Callbacks must honor cancellation after failed renewal.


### Credential and master-key rotation

`POST .../connections/{id}/credential-rotations` revalidates its own write-only credential
and rejects a different provider account. The new immutable version and audit
commit together; retained versions remain available for legitimate submitted
reconciliation. There is no credential cache registered. Version-scoped queued
work must use `connection.RequireVersion` before dispatch; rotation does not
silently replace the credential bound to an old review.

Mount a keyring containing both old and new master keys, then run:

```sh
go run ./cmd/sama-keys-rewrap -old-key OLD_ID -new-key NEW_ID -batch-size 100
```

Each batch commits independently (maximum 200 versions). Rerunning resumes from
remaining old-key references, including revoked and inactive credential versions.
Only the wrapped data key and wrapping nonce change; ciphertext, identity and
provider credential version remain unchanged. The command reports all retained
database references. Retain old keys until all installations and retained backups
no longer need them; it never deletes mounted keys or proves backup retirement.

Claimed version-2 refresh jobs can be passed to `inventory.Scanner` with an
explicit read-only adapter factory. Scans recheck the stored actor, grants,
connection/version and lease on every page and stage allowlisted observations.
Bounds are 100 pages, 200 rows/page, 10,000 rows, 8 MiB/page, 32 MiB total and
five minutes (or the earlier job/caller deadline). Partial scans do not alter
public inventory. Legacy actorless syncs fail closed. This does not start a
scheduler or register a production provider; those remain separate gates.
`Scanner.Execute` publishes only a completed scan in the same transaction as its
terminal sync/job state. Failed scans retain the last complete inventory; missing
resources are tombstoned only within the fully scanned scope. Older generations
cannot replace newer completed scans, including newer empty scans.

`GET /api/v1/workspaces/{workspace_id}/syncs/{sync_id}` reads authorized progress
without provider traffic. Sync and connection views include last-success time,
allowlisted failure category and `fresh`, `stale` or `never_observed` state. Failed
refreshes preserve successful timestamps; the freshness target is five minutes.

`GET .../resources` reads granted complete local inventory with `connection_id`,
`account`, `type` and `region` filters. Pages default to 50 rows (maximum 200).
Pass the opaque `next_cursor` unchanged; scope/actor/filter changes invalidate it.
Unknown, duplicate, empty and malformed query values return a safe 400.
`GET .../resources/{resource_id}` returns allowlisted version-1 server details,
native/normalized status, observation/freshness and advisory action reasons.
Missing, tombstoned and out-of-scope IDs return 404. Later dispatch must recheck
current authority; an action projection is never an acceptance token.

### Hetzner Cloud qualification boundary

Reviewed 2026-10-12: Cloud API v1 uses a project token with bearer authentication;
select Read permission for inventory. See the official [token guide](https://docs.hetzner.com/cloud/api/getting-started/generating-api-token/)
and [Cloud client](https://github.com/hetznercloud/hcloud-go/blob/main/hcloud/client.go).
The official [server client](https://github.com/hetznercloud/hcloud-go/blob/main/hcloud/server.go)
uses GET `/servers` and `/servers/{id}` with page/per-page options. Its
[server response schema](https://github.com/hetznercloud/hcloud-go/blob/main/hcloud/schema/server.go)
does not expose a stable account/project identity. That identity requirement is
unresolved: the documented token scope alone cannot prove account continuity
when rotating credentials. Do not substitute a token hash, provider-wide constant
or browser claim for verified provider identity. No Hetzner adapter is registered.
Provider rate limits, identity continuity and live inventory acceptance remain
unverified; existing transport deadlines, response caps and cooldowns stay required.

Set `SAMA_WORKERS_ENABLED=1` explicitly to start the registered sync scheduler;
PostgreSQL and provider-calling mode 1 are required. Default is off. Four workers
are the starting configuration (bounded by `SAMA_WORKER_COUNT`). A dedicated
PostgreSQL advisory-lock session excludes a second active scheduler. Loss of that
session stops work. SIGTERM stops claims, cancels reads and drains within the
configured shutdown deadline; interrupted leases remain recoverable. Only
registered job type/version handlers run. Until provider qualification is
resolved, the production sync reader registry remains empty and fails safely.

Operation storage includes actor/target/version-bound expiring reviews, immutable
accepted intent, scoped idempotency keys and explicitly sequenced events. Version-3
job payloads carry only connection/operation references; old versions stay valid.
The registered sync scheduler does not claim operation-dispatch jobs. No mutation
adapter or provider dispatch is enabled by these storage/acceptance primitives.
Operation transitions compare expected state and a live version-3 job lease, and
append their sequence in the same transaction. Unknown is not terminal; recovery
requires a new durable reconciliation audit reference. State persistence alone
never supplies provider dispatch authority or evidence of provider completion.
Reservations serialize one compute resource identity or creation slot per
operation. Unknown outcomes retain their reservation through lease expiry;
definitive terminal state and release commit together. Multi-resource intents
are rejected. A database fencing counter does not fence an external provider API.
Initial typed operation intents are the four server power actions with an explicit
empty parameters object. Hashes bind workspace, actor, connection, target and
review IDs to canonical typed intent; unknown fields/actions and secret-bearing
parameters are rejected. Defining an intent does not enable an adapter or dispatch.

`POST .../resources/{id}/action-reviews` accepts `{action,parameters:{}}` and
returns a five-minute server-owned review after fresh read-only preconditions.
Actor/target, credential and policy versions are bound; high-impact classifications
require verified recent authentication and an explicit high-impact grant. No
production reviewer is registered until action-specific qualification is complete.
Confirmation first resolves the actor-scoped idempotency key, even for a consumed
review. New acceptance rechecks current grants/versions and fresh provider
preconditions, then consumes the review and writes operation, reservation, job,
event and audit atomically. Queue quotas apply; a failed write rolls everything
back. Duplicate keys with different canonical intent return a conflict.
`POST .../operations` requires `Idempotency-Key`, `{review_id,confirmed:true}`
and, for a high-impact review, `typed_target` plus verified recent authentication.
It returns 202 with an operation reference and Location. The operation remains
queued until a separately qualified dispatcher is implemented; no provider
mutation is performed by confirmation or by the sync scheduler.
A reclaimed/interrupted scan discards its unpublished staging and allocates a new
attempt generation, retaining the same durable sync reference and admission
order. The stale lease cannot write or publish later pages. No provider cursor
is assumed restart-safe; interrupted reads restart from the first bounded page.

`go run ./cmd/sama-metadata-cleanup` runs database-only batches of at most 100
rows per class: unconsumed/unreferenced reviews expired for seven days, completed
sync staging older than seven days and eligible terminal jobs completed more than
seven days ago. Unknown/in-flight operations, reservations, accepted reviews,
operation/idempotency rows and audit/events are retained. Idempotency evidence
therefore remains beyond its 30-day minimum; audit/event retention is not shortened.
Legacy jobs without a proven completion timestamp are not age-pruned. Repeat the
bounded command deliberately for further batches; it makes no provider requests.

Documented read adapters and the explicit disposable read-probe command are
covered in [provider qualification](provider-qualification.md). Typed write-only
credentials now include `contabo_v1` alongside `bearer_v1`. All production
provider registrations remain closed until stable identity and live qualification
are established; mocks never enable capabilities.

`GET .../audit-events` reads current permission-filtered local history with
`actor_id`, `action` and `target_id` filters. Pages default to 50 (maximum 200),
ordered by descending timestamp and UUID; cursors bind actor, workspace and
filters. Connection and operation events require a current read grant; other
workspace events are visible to administrators or their actor. Historical actor
IDs survive membership removal, with only allowlisted metadata returned.
`GET .../connections/{id}/grants?user_id={member_id}` returns management-only
member grants and server-derived delegation ceilings. PUT rechecks authority.
Connection save responses now include their authoritative validation identity,
verification time and capabilities alongside the connection and sync references.
