# Architecture decision records

Draft date: **2026-09-04**. These records are proposals for user review, not approved implementation decisions. Go, React, Webpack, one repository, separate backend/frontend folders, and no payment handling are user requirements. Record acceptance of the remaining choices only after review.

## ADR-001: Go modular monolith — proposed

**Context:** Many providers introduce integration complexity; a new project has limited operational capacity.
**Decision:** One Go module and executable under `backend/`, domain-owned internal packages, adapter boundaries, one repository.
**Alternatives:** Microservices, a generic API proxy, or a serverless function per action.
**Consequences:** Simpler local development, transactions, and releases; module discipline is necessary. Extract services only after measured scaling/isolation needs.
**Validation:** The second provider must fit without moving its native semantics into the shared domain.

## ADR-002: React SPA with Webpack — proposed

**Context:** Interactive management workflows with no SEO requirement, and an explicit preference for Webpack.
**Decision:** React + TypeScript + Webpack under `frontend/`, with its own package manifest, lockfile, configuration, source, and tests; same-origin API access; no Node runtime required in deployment.
**Alternatives:** Vite, Next.js, server-rendered templates.
**Consequences:** Explicit build configuration and bundle budgets; authentication/state handling must be designed for a SPA. **Validation:** Production browser checks against Go, not only development-server success.

## ADR-003: PostgreSQL as the only initial durable store — proposed

**Context:** Jobs, sessions, credentials, inventory, and audit need transactions and crash recovery.
**Decision:** PostgreSQL 18 and a database-backed worker queue.
**Alternatives:** SQLite, Redis queue, external broker.
**Consequences:** One extra process for self-hosting; one database dialect and one consistency model. SQLite simplicity loses priority to the cost of maintaining equivalent job/isolation behavior twice.
**Validation:** Disposable PostgreSQL transaction, lease, uniqueness, restore and migration tests before implementation claims.

## ADR-004: Capability-based adapters — proposed

**Context:** Providers differ in authentication, products, state transitions, and action support.
**Decision:** Small typed domain ports; distinct implemented, connection-permitted, and resource-available capabilities; native features preserved.
**Alternatives:** Lowest-common-denominator universal interface, raw provider passthrough.
**Consequences:** More explicit per-provider mapping, honest UX, bounded API surface.
**Validation:** Hetzner plus DigitalOcean inventory and power workflows before extracting a generalized mutation adapter.

## ADR-005: OIDC and encrypted server-held credentials — proposed

**Context:** Sama needs user identity independent of provider accounts and must use credentials after the browser closes. **Decision:** OIDC with server-side sessions, workspace grants, envelope-encrypted credentials, external keyring. **Alternatives:** Custom password/MFA subsystem, browser-only provider tokens, plaintext env-based shared credentials. **Consequences:** Self-hosters configure an identity provider; credentials remain high-value runtime material. A separate OIDC bootstrap guide is part of the identity milestone.
**Validation:** Session, CSRF, cross-workspace, key rotation, backup and redaction tests before accepting secrets.

## ADR-006: Reviewed asynchronous mutations — proposed

**Context:** Provider changes can cost money, disrupt services, and complete after network timeouts.
**Decision:** Bound review → durable operation → guarded dispatch → provider observation; unknown outcomes block unsafe repeats.
**Alternatives:** Synchronous direct writes, blanket retries, optimistic success.
**Consequences:** Some operations require reconciliation/manual resolution; no universal exactly-once promise. **Validation:** Fake-provider fault injection and live action-specific acceptance.

## ADR-007: Focused rollout and limited AWS scope — proposed

**Context:** Supporting every listed provider/product at once risks superficial integrations.
**Decision:** Hetzner → DigitalOcean → Vultr/Contabo, Netcup qualification, then S3 family; SQS/SNS later.
**Alternatives:** All providers at launch, full AWS management first.
**Consequences:** Smaller initial coverage and explicit evidence per capability, with a broad architecture that can expand. **Validation:** Roadmap gates and a capability matrix that never labels research as implemented.

## ADR-008: Apache-2.0 and local-first project setup — proposed

**Context:** The project is intended to be open source but has no chosen remote.
**Decision:** Apache-2.0 license, no assumed GitHub organization/module path, project-level contribution and security documents; CI and release tooling deferred until implementation.
**Alternatives:** MIT, copyleft licensing, immediately selecting a hosted organization.
**Consequences:** A permissive project license with an explicit patent grant; maintainer identity, reporting contacts, governance, and release infrastructure remain public-launch tasks. The owner can revisit licensing before accepting third-party contributions.
**Validation:** License file present; no fabricated contacts, remote URLs, or affiliation claims.

## ADR-009: Separate backend and frontend projects — user requirement

**Context:** The repository should be easy to navigate without mixing Go and frontend files at the root.
**Decision:** Keep Go source, modules, migrations, API contracts, and backend tooling under `backend/`; keep React source, Webpack/TypeScript configuration, npm dependencies, and frontend tooling under `frontend/`. Keep global documents at the root and architecture in `docs/`.
**Consequences:** Each project has a clear ownership and build boundary; the HTTP contract connects them. A future release may assemble their outputs without merging their source trees.
**Current phase:** Document the layout only; create these directories when implementation is explicitly requested. See [repository structure](../architecture/repository.md).

## ADR-010: Provider payments remain outside Sama — user requirement

**Context:** Sama is an API-based console for managing products in users' own provider accounts.
**Decision:** Allow supported resource creation and management while keeping all billing and payment handling with the provider. No checkout, payment processing, payment-method storage, top-ups, invoice settlement, refunds, or resale billing belongs in Sama.
**Consequences:** A resource action may increase the user's provider bill; Sama may explain that impact but cannot execute a payment. If the provider requires billing action, the user completes it in the provider's portal. This boundary also applies to future integrations and Pro features. See [product scope](../product.md#payments-stay-with-the-provider).
