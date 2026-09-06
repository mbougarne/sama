# Deployment, reliability, and operations

## Selected deployment architecture

This document specifies the selected operational design and the evidence required to qualify a deployment.

The initial management release uses a TLS reverse proxy, Sama, and PostgreSQL. Backend and frontend source and builds remain separate under `backend/` and `frontend/`. The release assembly step will package the Go binary and built frontend assets in one image for simpler self-hosting. This does not require either source tree to contain the other; deployment-level assembly would belong under `deploy/` when implemented.

Mount an encryption keyring and OIDC configuration. Keep PostgreSQL off public networks, expose only the proxy, and run the app as a non-root user with a read-only filesystem. HTTPS and a configured public origin are mandatory for authenticated deployments.

The baseline has one provider-calling Sama process. Additional API processes can serve DB-backed/local requests, but they must not independently validate credentials, prepare provider-backed reviews, run syncs, dispatch mutations, or reconcile under process-local rate limits. Before any of those paths scale horizontally, introduce a shared provider-scope concurrency/cooldown budget or route every provider call through one designated scheduler and verify failure behavior.

## Configuration design

Target settings: public origin, database URL/file reference, OIDC issuer/client ID/client-secret file, keyring file, worker mode, request/job budgets, retention, provider/action enablement, and trusted proxy ranges. Validate at startup; redact secret values from errors. Credentials use mounted files where possible, not checked-in YAML. No live credentials in frontend environment variables.

Use separate runtime and migration database identities. Runtime gets only needed DML and insert-only audit access. DB pool starting limit: 10 connections per app process, adjusted against server capacity and replica count. Configure statement/transaction timeouts and avoid idle-in-transaction sessions. Cache non-sensitive catalogs in process; PostgreSQL remains authoritative for sessions, jobs, permissions, and operations.

## Cache policy

PostgreSQL is the only required durable service. Its job queue is about reliable work execution; caching is about reducing repeated reads. Neither implies the other. Inventory observations stored in PostgreSQL already keep ordinary UI reads off provider APIs. They are durable last-known state with freshness metadata, not an expendable Redis cache.

Use small, disposable in-process caches for reproducible non-sensitive catalog data such as regions and plan descriptions. Initial bounds: five-minute TTL, at most 1,000 entries and 16 MiB per process, eviction at either bound, and coalescing of concurrent loads of the same key. Treat these as tuning targets. Pricing displayed during an action review must be revalidated against the provider when possible or explicitly marked unavailable/stale; a cache never authorizes spend or guarantees a price.

Account-specific cache keys include workspace UUID, connection UUID, API family, scope/region, and the relevant credential/configuration version. Only demonstrably public catalogs may use global keys. Invalidate scoped entries after connection rotation/disabling and configuration changes. Cache neither credentials nor authorization decisions here. The specialized short-lived adapter token lifecycle is governed by the security design, not a generic catalog cache.

PostgreSQL remains authoritative for sessions, memberships/grants, operation state, idempotency, resource reservations, audit, and job leases. Revalidate authorization at each sensitive action and dispatch. Refresh coalescing across processes uses the existing durable sync-job identity/constraints; process-local request coalescing alone is insufficient across replicas. Each replica can rebuild its optional cache after restart without losing work.

**Redis is not part of the baseline.** Consider a shared evictable cache only if measured repeated DB/provider reads or replica duplication remain a bottleneck after indexing, bounded paging, and coalescing. Evidence should identify the workload, hit rate, memory cost, expected latency/throughput gain, invalidation design, and behavior when the cache is unavailable. The owner must authorize the addition through an ADR amendment. Redis would not replace PostgreSQL or become authoritative for operations or permissions; losing the cache must never lose accepted work.

## Health and lifecycle

The planned `/health` reports process liveness. `/readyz` should require compatible schema, a usable DB pool, required key IDs, and valid identity configuration. Individual provider outages should degrade that connection, not fail whole-app readiness. Liveness should not restart healthy processes merely because a provider is down.

On SIGTERM, stop accepting new requests and claiming jobs, allow a bounded HTTP drain period, and checkpoint worker state before exit. Worker shutdown must leave durable dispatch markers intact. Use a reverse proxy with suitable request size/time limits and trust forwarded headers only from that proxy.

## Observability

Required observability includes structured startup/shutdown/error logs, per-response request IDs, and request counts/latency/status by route template; DB pool saturation; queue depth/oldest job; operation outcome, unknown count and age; lease expiry; provider latency/error/rate-limit counts; sync lag and stale scope count. Avoid high-cardinality user/resource IDs as metric labels. Logs can use safe opaque correlation IDs.

Prometheus metrics are private/admin-only. OpenTelemetry export is opt-in, excludes secrets and raw request bodies, and has explicit deployment-owned destinations. Provider names, request templates, and normalized errors are sufficient for most diagnosis. No hosted telemetry by default.

Alert on rising unknown operations, queue age, sustained connection authorization failure, missing key IDs, failed backups, DB connection exhaustion, and scope freshness violations. Separate accepted operation rate from completed success rate; an HTTP 202 success graph must not hide provider failures.

## Initial budgets and measurement

These are **targets to test**, not current benchmarks:

| Measure | Initial objective and condition |
| -------------------- | ----------------------------------------------------------------------------------------------------------------------------------------- |
| Cached inventory API | p95 below 200 ms at 20 concurrent users, 10,000 observed resources across 20 connections, on a 2 vCPU/2 GiB app host and healthy local DB |
| Mutation acceptance | p95 below 300 ms for confirmation of a valid stored review; excludes preparing fresh provider state |
| Inventory freshness | Target within five minutes under normal provider availability and available rate budget; display actual lag |
| Request concurrency | Four background workers initially, two upstream requests per connection, configurable within provider quotas |
| Browser payload | Webpack entry below 350 kB uncompressed; individual assets below 250 kB |
| Recovery | Restart preserves accepted operations; no blind duplicate external writes after injected crashes |
| Storage | Paginated observations; bounded details JSON, job payloads and retention; no object content caching |

Do not claim a fixed number of providers/resources supported without a workload test. Load test the database cache independently of the provider to avoid spending cloud quota on artificial traffic. Measure sync under throttling and partial failures separately.

## Backup and restore runbook (management release)

1. Back up PostgreSQL with encrypted backups and an external retention policy. Start with daily backups; add WAL archiving/PITR if the deployment requires tighter recovery. Baseline target RPO 24 hours/RTO four hours; production operators must choose whether that is acceptable.
2. Back up the keyring through a separate secure channel; record key IDs required by retained snapshots. A DB-only backup cannot restore credentials. Store manifests and schema/application versions with backups, never plaintext keys.
3. Restore into an isolated environment with provider egress and workers disabled. Verify schema and key availability, tenant counts, referential integrity, audit records, and sample credential decryptability without making provider calls.
4. Keep a global mutation dispatch gate closed. Restored operations may lag already-executed external actions; reconcile with read-only provider queries before enabling any writes.
5. Confirm representative resource state and resolve ambiguous work, then explicitly reopen dispatch. Record a restore drill before the first production release and after key/schema changes.

Sama cannot roll back cloud-side effects by restoring its database. Automatic compensation is not a generic feature; each action needs a separately reviewed compensating operation if one exists.

## Upgrade and release runbook

Run CI and adapter contract checks; build reproducible artifacts from lockfiles; generate an SBOM; scan dependencies/images; publish checksums and sign release artifacts when publishing infrastructure exists. Pin container base image digests at release time and automate reviewed updates. Never execute untrusted pull-request code with release credentials. Future pull-request CI should have read-only permissions and no access to publication credentials.

Before upgrade: backup + key manifest → stop claims/drain workers → apply forward migration with migration identity → start compatible app version → verify readiness and representative reads → reconcile pending operations → resume workers. Keep previously built images available. Roll back the app only if the current schema is compatible; otherwise restore and reconcile. Maintain explicit job payload versions so queued work survives application upgrades.

## Failure runbooks

- **Provider 429/outage:** cool down affected scope, show stale observations, retain jobs until bounded deadline; do not multiply retries across layers.
- **Credential revoked:** disable new dispatch, require administrator revalidation, preserve history and unknown operations.
- **Missing encryption key:** fail credential functionality closed; restore the correct key, never overwrite encrypted rows with a generated key.
- **DB unavailable:** reject new writes, stop job claims, preserve dispatch evidence; return retryable service status for local reads that require DB.
- **Unknown mutation:** keep target reserved, reconcile provider action/native resource, record evidence; never offer an unconditional retry button.
- **Compromised account:** revoke at provider, disable connection, invalidate related sessions if warranted, investigate audit references and revalidate before reopening.
