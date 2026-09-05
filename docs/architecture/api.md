# HTTP API and provider contracts

## Proposed contract ownership

This document specifies the API contract and adapter boundaries; implementations must be qualified against these requirements.

When implementation begins, the backend should own the source contract at `backend/api/openapi.yaml`. Generate frontend types into `frontend/src/api/generated/` from that contract. This is a contract-generation dependency, not a runtime import of backend code into the frontend. Changes to the contract should be reviewed alongside the affected feature.

Proposed operational routes are `GET /healthz` for process liveness and `GET /readyz` for readiness against required dependencies. Unknown API routes should return a JSON problem rather than frontend HTML. Metadata and capability endpoints should be introduced with an actual product use case; no placeholder provider directory is required.

## Target resource API

The API and provider adapters must expose resource management only. No payment, checkout, wallet, top-up, payment-method, invoice-settlement, refund, or billing-management endpoints belong in Sama. Do not call such provider endpoints indirectly or forward them through a generic proxy. Resource creation is permitted when the provider can bill the user's existing account directly; if a payment step is required, return an actionable blocker and send the user to the provider's portal. See [payment boundary](../product.md#payments-stay-with-the-provider).

All tenant routes are prefixed `/api/v1/workspaces/{workspace_id}` and require session authentication plus server-side membership checks. Keep `workspace_id` explicit so every request and cache key states its boundary. Do not trust it without resolving the authenticated principal.

| Method and relative path | Purpose and response |
| ------------------------------------------------------------ | -------------------------------------------------------------------------------------------- |
| `GET /connections` | Authorized connection metadata; credentials always omitted |
| `POST /connection-validations` | Bounded, rate-limited non-mutating credential validation; secret body never retained in logs |
| `POST /connections` | Create encrypted connection; 201 plus initial sync reference |
| `POST /connections/{id}/credential-rotations` | Replace write-only credentials; audit and revalidation |
| `POST /connections/{id}/disable` | Disable future dispatch; accepted provider work remains visible |
| `GET /connections/{id}/capabilities` | Effective implemented/verified/authorized capabilities and reason codes |
| `POST /connections/{id}/syncs` | Deduplicated durable refresh; 202 and sync run ID |
| `GET /resources?type=&connection_id=&region=&cursor=&limit=` | Cursor-paginated local observations with freshness |
| `GET /resources/{id}` | Shared fields, safe typed details, available actions, observation time |
| `POST /resources/{id}/action-reviews` | Prepare a server-stored review of typed action intent |
| `POST /operations` | Confirm a review with Idempotency-Key; 202 plus Location |
| `GET /operations/{id}` | State, safe progress, provider references, outcome and affected target |
| `GET /operations` | Authorized recent/history view with cursor |
| `POST /operations/{id}/cancel` | Atomic cancellation only before dispatch; 409 otherwise |
| `POST /operations/{id}/reconciliations` | Audited recheck for unknown outcomes; never unconditional retry |
| `GET /audit-events` | Workspace-scoped, permission-filtered activity |

Identity routes live outside this prefix: `/auth/login`, `/auth/callback`, `/auth/logout`, `/api/v1/me`; workspace and membership routes are introduced with identity. Credentials, user tokens, provider response bodies, and unrestricted signed download URLs never appear in general resource DTOs.

## Shapes and conventions

Use snake_case JSON fields, RFC3339 UTC timestamps, UUIDv7 Sama entity identifiers represented as JSON strings, opaque provider native identifiers, and discriminated action inputs. Lists return `{data: [...], next_cursor: null | string}`. A resource returns shared identity, display name, provider/account/region, normalized status, native status, observation timestamp, freshness, and a typed details object. Never overload `null` to mean both unsupported and unknown; expose explicit availability/status reasons.

Errors follow an application/problem+json envelope: `type`, `title`, `status`, `code`, `request_id`, and optional allowed-field validation errors. Document this shape in the future OpenAPI contract. Stable codes include `permission_denied`, `unsupported_capability`, `stale_review`, `resource_busy`, `provider_rate_limited`, `provider_unavailable`, `outcome_unknown`. Never forward upstream error text without sanitization.

Use 400 for malformed input, 401 for no session, 403 for authorization denial, 404 for absent/out-of-scope targets, 409 for state/idempotency conflicts, 422 for valid syntax with invalid action parameters, 429 for quotas/rate limits, and 503 for readiness or temporary service failure. 202 means durable acceptance, not provider completion. Return an operation Location and request ID. HTTP success codes do not override an operation’s state.

Mutation requests include CSRF header and `Idempotency-Key`; confirmation carries only the stored `review_id` and required confirmation evidence, not an editable copy of reviewed parameters. Idempotency and review rules are in [operations](operations.md). Safe resource views may later use private ETags; never cache auth, credential responses, or sensitive data at shared proxies.

## Provider adapter ports

Proposed responsibilities for the first integration:

| Domain port | Responsibility | Result |
| --- | --- | --- |
| Inventory reader | List servers in a bounded scope and retrieve an individual server | A normalized page or resource observation |
| Power review | Inspect current state and validate a typed action | Preconditions and impact information |
| Power submission | Submit the reviewed action using its retry policy | A provider receipt or explicit ambiguity |
| Power observation | Poll/reconcile a submitted action | Pending, definite success/failure, or insufficient evidence |

Create separate capability interfaces for snapshots, DNS, object storage and messaging when needed. Avoid a mandatory interface requiring every provider to implement every feature with `unsupported` stubs. The service resolves a configured, immutable adapter binding for a single connection. Context includes deadline/cancellation; actor authorization is resolved before constructing the binding, not read from arbitrary provider arguments.

`Submission` records native action ID, safe request reference, correlation method, and retry classification; it never embeds credentials or arbitrary raw JSON. `Outcome` distinguishes pending, definite success, definite failure, and insufficient evidence. Normalize provider errors into typed categories with optional safe RetryAfter, provider request ID, and an `Ambiguous` flag. Never make a generic `Retryable=true` sufficient to retry a mutation.

Transport shares TLS, validated endpoints, timeouts, size caps, redaction, and bounded retry mechanisms. Authentication, pagination interpretation, resource mapping, quota scope, and completion semantics remain provider-specific. SDK clients may be used behind these interfaces after reviewing their default retries, logging, endpoint overrides, and credential lookup behavior.

Entity fields named `id` and `*_id` carry UUIDs, not integers. Parse and validate canonical UUID strings at the boundary; UUID possession does not grant access. Provider-specific identifiers remain separate opaque values. API DTOs expose no alternate numeric lookup key. See [identifier policy](data.md#identifier-policy).

## Compatibility and evolution

Keep `/api/v1` additive for stable fields. Breaking field meaning, action semantics, or authentication contracts require an explicit migration/version decision. Provider schema evolution is isolated in adapters and fixture versions. Unknown native statuses become normalized `unknown` while retaining safe native text; they never default to `running` or `succeeded`. Check generated TypeScript types into Git and fail CI if regeneration differs. Contract tests validate actual Go responses rather than merely revalidating example JSON.
