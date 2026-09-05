# Architecture decision records

Stabilization date: **2026-09-05**. The owner instructed the project to use the existing ADR decisions rather than their alternatives. Records 001–007 are now accepted architectural direction; 009–010 remain explicit user requirements. Record 008 is revised to match the requested simple permissive licensing. The cache clarification in 003 and records 011–012 are selected refinements in response to this review, not claims that the owner explicitly named each library or UUID version.

Acceptance or selection here describes the design only. Nothing is implemented, benchmarked, or production-qualified. New implementation still requires an explicit request. Historical alternatives are recorded only to explain decisions, not offered as competing plans. The core Go, React + TypeScript, Webpack, and PostgreSQL stack is fixed; improvements and security/version updates within it remain in scope.

## ADR-001: Go modular monolith — accepted

**Decision:** One Go module and executable under `backend/`, with domain-owned internal packages and provider adapter boundaries. Keep one repository.

**Reason:** Straightforward local development, transactions, and releases with clear ownership. Provider integrations do not each need a microservice. A generic API proxy is not the product.

**Consequences:** Maintain module boundaries and bounded worker resources. Separate runtime roles only when deployment needs them; this is not permission to replace Go or the selected stack.

**Future evidence:** The second provider fits without moving its native semantics into shared domain logic.

## ADR-002: React SPA with Webpack — accepted

**Decision:** React + TypeScript + Webpack under `frontend/`, with its own manifests, configuration, assets, and tests. The backend and frontend communicate through an HTTP contract. Same-origin serving is the deployment baseline; Node is build/development tooling.

**Reason:** Interactive resource workflows need no SEO-oriented server rendering. The owner has fixed the stack. Other languages, frameworks, and bundlers are not implementation options.

**Consequences:** Build backend and frontend independently; assemble their outputs into the release image without mixing source trees. State ownership is specified in ADR-011.

**Future evidence:** Browser workflows against production packaging, strict type checking, accessibility, and bundle budgets.

## ADR-003: PostgreSQL for durable data and jobs — accepted; cache policy clarified

**Decision:** PostgreSQL 18, explicit migrations, and a database-backed job queue. No Redis dependency in the initial architecture. Use bounded in-process caching for safe, reproducible catalog reads and TanStack Query for browser server state.

**Reason:** A job queue and a cache solve different problems. PostgreSQL persists operations, leases, sessions, credentials, inventory observations, and audit events. Reading persisted inventory avoids waiting for upstream APIs on every page view. A small disposable cache can reduce repeated catalog reads without another service.

**Consequences:** Commit intent, job, and audit together. PostgreSQL's `SKIP LOCKED` supports avoiding row-lock contention when multiple consumers claim queue work; this is not exactly-once provider execution. [PostgreSQL SELECT documentation](https://www.postgresql.org/docs/current/sql-select.html)

**Redis boundary:** A shared cache may later be useful if measured multi-replica duplication or repeated reads remain costly after query/index tuning and coalescing. Adding one requires evidence and an explicit architecture amendment, not a language/database replacement. It would contain evictable derived data only; PostgreSQL remains authoritative for all durable state, idempotency, reservations, and authorization. See [cache policy](../architecture/deployment.md#cache-policy).

**Future evidence:** Real PostgreSQL concurrency, lease/restart, migration, and restore checks; representative latency and cache-hit/memory measurements. “Enough initially” is the design judgment, not a completed load-test result.

## ADR-004: Capability-based adapters — accepted

**Decision:** Small typed domain ports. Distinguish provider features, adapter implementation, verified connection permissions, Sama grants, and resource-state restrictions. Preserve native features.

**Reason:** Uniform interaction does not imply uniform provider behavior. Neither a lowest-common-denominator interface nor raw provider passthrough meets the goal.

**Future evidence:** Hetzner and DigitalOcean share applicable domain checks while retaining provider-specific paging, action, and error cases.

## ADR-005: OIDC and encrypted server-held credentials — accepted

**Decision:** OIDC with opaque server-side sessions, workspace/connection grants, envelope-encrypted provider credentials, and a keyring outside the database.

**Reason:** User identity is independent from provider identity. Durable jobs must work after the browser closes without storing provider credentials in the browser.

**Consequences:** Self-hosters configure an identity provider; a bootstrap guide and disposable identity setup belong to the first implementation milestone. Encryption does not protect a compromised running host from using credentials.

**Future evidence:** Session/CSRF/isolation, key rotation/restore, log redaction, and revoked-authority checks.

## ADR-006: Reviewed asynchronous mutations — accepted

**Decision:** Bound review → durable operation → guarded dispatch → provider observation. Unknown outcomes prevent unsafe repeated changes.

**Reason:** Cloud operations can complete after a timeout and may incur provider charges or disrupt services. Blind retries and optimistic success are unsuitable.

**Consequences:** Reconciliation and sometimes manual resolution are necessary. No universal exactly-once claim; no payments are handled by Sama.

**Future evidence:** Crash/ambiguity tests plus disposable, action-specific provider acceptance.

## ADR-007: Focused rollout and limited AWS scope — accepted

**Decision:** Hetzner → DigitalOcean → Vultr/Contabo; qualify Netcup's separate APIs; add S3-family support before SQS/SNS extensions.

**Reason:** Qualify useful capabilities rather than claim every provider/product at launch. This sequence does not limit the long-term product vision to inventory.

**Future evidence:** Capability-level support records, provider outage isolation, and explicit unknown/unsupported states.

## ADR-008: MIT and local project setup — selected revision

**Decision:** Replace the initial Apache-2.0 proposal with the standard MIT license, matching the owner's request for a short permissive license and warranty/liability disclaimer. No custom license clauses. No assumed remote owner or hosted publication.

**Permissions and condition:** MIT permits use, modification, distribution, sublicensing, resale, and proprietary derivatives; retain its copyright and permission notice in copies or substantial portions. It is permissive, not literally condition-free. Its standard text includes warranty and liability disclaimers. [MIT license](https://opensource.org/license/mit)

**Tradeoff:** MIT is shorter; it does not contain Apache-2.0's express patent-grant section. The selection prioritizes the requested simplicity, without claiming identical legal provisions. [Apache-2.0 text](https://opensource.org/license/apache-2.0)

**Operational guidance:** Evaluate future releases in an isolated sandbox before production and use minimum provider permissions. Keep that guidance in security/deployment documents rather than turn it into a license restriction. No license promises immunity from every liability under every applicable law.

**Boundary:** Others may sell Sama-based products under the license. Sama itself still does not handle payments. Contributor conduct rules do not restrict licensed software use.

## ADR-009: Separate backend and frontend projects — user requirement

**Decision:** All Go source, modules, migrations, API contracts, and backend tooling belong under `backend/`. All React source, Webpack/TypeScript configuration, npm dependencies, and frontend tooling belong under `frontend/`. Global documents stay at the root; architecture stays in `docs/`.

**Consequences:** Independent source/build ownership with an HTTP contract between them. Release assembly can combine their outputs without merging source trees. [Repository structure](../architecture/repository.md)

**Current phase:** Document the layout only; do not create implementation folders yet.

## ADR-010: Provider payments remain outside Sama — user requirement

**Decision:** Manage supported products in the user's own provider account; never handle payment collection/processing, checkout, payment-method storage, top-ups, invoice settlement, refunds, or resale billing.

**Consequences:** Resource creation may increase the user's provider bill. Sama can explain that impact; the provider handles billing and payments. Any required billing action is completed by the user in the provider portal. This also applies to future integrations and Pro features. [Product boundary](../product.md#payments-stay-with-the-provider)

## ADR-011: Frontend state has explicit owners — selected refinement

**Decision:** TanStack Query for server data; React Router for URL/navigation state; React local state and reducers for interactions and forms; narrowly scoped context for shared UI state. No Redux/Zustand store or second copy of server data in the initial design.

**Reason:** Inventory, operations, connection metadata, and permissions are server state. Treating them as a manually synchronized global store creates duplication. UI drafts and navigation have different lifecycles.

**Consequences:** Explicit query defaults, scoped keys, invalidation after mutations, and sensitive-cache removal on logout or scope change. Persist only allowlisted cosmetic preferences. Details and evidence requirements live in [frontend state management](../architecture/frontend.md#state-management).

## ADR-012: UUIDv7 entity identifiers — selected refinement

**Decision:** Use one UUIDv7 per Sama entity for its PostgreSQL key and API identifier, named `id`. Foreign keys use native PostgreSQL `uuid`; JSON represents them as strings. No numeric-plus-public-UUID mapping by default.

**Reason:** The original `id` field names did not specify integers; the data model already called for UUIDs. UUIDv7 makes the version explicit and provides time ordering. PostgreSQL supports a native UUID type and version-7 generation. [PostgreSQL UUID documentation](https://www.postgresql.org/docs/current/functions-uuid.html)

**Consequences:** No blanket claim of faster queries. Benchmark the real workload before considering a table-specific numeric surrogate, and preserve public UUIDs if that ever becomes necessary. UUIDv7 reveals approximate generation time and is not a secret or access control. Timestamps/sequences establish event order; tokens use independent cryptographic randomness. See [identifier policy](../architecture/data.md#identifier-policy).
