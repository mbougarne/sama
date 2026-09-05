# Implementation roadmap

Milestones define delivery order and acceptance criteria. They are not a live task tracker or authorization to start work; task scope and progress belong in collaboration records.

**All milestones exclude payment and billing management.** Provisioning may create provider-billed charges, but Sama must never handle payment. Any required checkout, top-up, or billing-account action is completed directly with the provider. This is a fixed product boundary, not a feature deferred to a later release or Pro tier.

## M0 — Architecture stabilization

Establish the component boundaries, separate backend/frontend ownership, product scope and security requirements recorded in the [decisions](decisions/README.md). Keep the chosen stack fixed. Revisit design through explicit amendments when evidence warrants a change.

Acceptance: decisions and consequences are documented consistently, provider uncertainties have a qualification path, and the [collaboration contract](agents/README.md) gives contributors a common workflow.

## M0.1 — Repository scaffolding

Create separate `backend/` and `frontend/` projects according to [repository structure](architecture/repository.md). Each owns its source, dependency manifests, configuration, build output and tests. Shared hooks and CI invoke each project's checks.

Acceptance: each side builds and checks independently; the HTTP contract is the integration boundary; the root contains shared project material rather than application source. Formatting, lint, meaningful tests and builds run locally and in CI.

## M1 — Identity and durable workspace foundation

Depends on M0.1 and the agreed design. Implement PostgreSQL migrations and runtime DB role; OIDC code/PKCE flow, session storage, bootstrap owner, memberships and connection grants, CSRF and same-origin checks, audit persistence. Supply a local disposable OIDC setup and documented external issuer configuration.

Acceptance: login/logout/expiry/revocation and last-owner rules; cross-workspace negative tests; schema constraints and failed audit transaction behavior against real disposable PostgreSQL; startup/readiness failure for invalid identity/schema; fresh install and upgrade paths. No real provider credentials yet.

## M2 — Secure connections and Hetzner read-only inventory

Depends on M1. Implement keyring/envelope encryption, credential versions/rotation, non-mutating validation, outbound transport protections, Hetzner account binding and server list/get, durable coalesced sync jobs, observation generations, inventory/details UI and freshness indicators.

Acceptance: wrong-key/AAD/rotation/restore/redaction tests, failed page walks preserve last good resources, expired token/403/429 coverage, no cross-connection credential use. Opt-in disposable Hetzner read-only acceptance with recorded scope and expected resources. Credentials remain disabled in ordinary fixtures; no mutation endpoints.

## M3 — DigitalOcean inventory and abstraction review

Depends on M2. Add a second provider with independent credential scope and paging/status mapping. Revisit common resource and adapter contracts using both providers. Add cross-provider filters and connection-specific sync failures.

Acceptance: both adapters pass the same applicable domain contract suite plus native cases; one provider’s outage leaves the other inventory usable; unsupported/unknown/denied capability states are distinct. No generalized mutation layer approved solely from the first provider.

## M4 — Durable power operations

Depends on M3. Add typed reviews, risk confirmation, atomic operation/job/audit acceptance, idempotency, resource reservations, guarded dispatch, polling/reconciliation, unknown outcomes, cancellation before dispatch, and operation history UI. Start with one provider/action, then qualify the second provider independently.

Acceptance: crash matrix in [operations](architecture/operations.md), real PostgreSQL concurrency tests, no duplicate mutating calls after ambiguity, reauthorization after membership/credential changes, failed confirmation-response recovery, and cleanup verified on disposable live resources. Reboot is not successful merely because a server is running. No create/delete/rebuild until separate gate.

## M5 — Additional providers and action breadth

Depends on M4; Netcup research can occur earlier without credential use. Add Vultr and Contabo read-only first, then individual qualified actions. Produce a Netcup compatibility report covering current SCP REST schema/auth, fallback need, and separate legacy DNS/CloudDNS behavior. Add snapshots/volumes/network controls in their own slices.

Acceptance: adapter checklist complete per capability, verified token refresh/rate/paging behavior, explicit unsupported product limits. DNS edits use reviewed record-set diffs and detect concurrent changes; never overwrite a whole zone from a stale cache. Keep domain purchasing/reselling deferred.

## M6 — Controlled provisioning and object storage

Depends on the durable mutation gate. Add provider catalogs, current pricing references, plan/region/quota validation, reviewed create/resize/rebuild/delete, deletion protection, and operation-specific cleanup/recovery. Separately add S3-family bucket metadata and scoped object listing, with separate credentials and endpoint profiles.

Acceptance: fresh pricing/unknown estimate behavior, spend review, no duplicate creates, definite vs ambiguous deletion, region constraints, object-list permissions without unnecessary data-read access. Upload/download/presigning requires its own content security, size, expiry, audit and bandwidth tests. IAM policy editing, recursive deletes, queue receives, publishes, and purges stay disabled unless independently designed and qualified.

## M7 — Public release hardening

Depends on the capabilities included in the chosen release. Test representative load, accessibility and keyboard/zoom flows, install/upgrade/restore, provider outages, key rotation, and two-workspace isolation. Complete a focused security review before broad credential use.

Choose the real GitHub owner/module path; configure private reporting and moderation contact, required CI checks/branch protection, trusted maintainers, security support window, signed releases, SBOMs, image digest pins and dependency checks. Test amd64/arm64 containers. Publish capability-level support and limitation tables, self-hosting/OIDC instructions, backup runbook, and release notes.

## Later candidates

AWS SQS/SNS configuration inventory and carefully scoped management; broader S3 providers; team approval workflows; Arabic/RTL localization; SSE progress; scoped automation; cost visibility; external audit export. Each needs a product case and its own data-access/risk model. No assumption that these are required for v1.
