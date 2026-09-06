# Security architecture

**Accepted security design, awaiting implementation.** This repository contains documentation only. None of the controls below is implemented or empirically validated; they are requirements to satisfy before operating a credential-bearing application.

## Trust model

The browser is untrusted. Provider responses, resource names, tags, and errors are untrusted content. Workspace members can be malicious; connection credentials can be overprivileged; cloud actions can complete after network failure. A compromised Sama host can access credentials in use and act with its provider permissions. Encryption at rest cannot make a compromised runtime safe. Prefer least-privilege provider accounts and separate deployments for stronger administrative isolation.

| Threat | Required control | Proof before enabling credentials |
| -------------------------------------------- | ---------------------------------------------------------------------- | ------------------------------------------------------------------------------------------ |
| Cross-workspace resource or operation access | Server-side tenant scope and connection grants, composite foreign keys | Two-workspace negative tests for every identifier-bearing API and background job |
| Database/backup theft | Envelope encryption, keyring outside database, encrypted backups | Wrong workspace/connection AAD cannot decrypt; restore with/without keys behaves correctly |
| Browser credential theft | HttpOnly sessions, strict CSP, no provider secrets in client storage | Inspect browser network/storage, XSS fixtures, logout/revocation tests |
| Forged or replayed mutation | Origin/CSRF checks, bound one-use review, idempotency | Cross-origin rejection, expired/replayed review, changed intent/target/actor tests |
| Duplicate cloud action after timeout/crash | Durable state machine and conservative ambiguity handling | Fault injection immediately before/after provider submission |
| SSRF or metadata credential exposure | Approved endpoints, safe resolver/dialer, redirect controls | IPv4/IPv6 private, loopback, link-local, mixed DNS answers and redirect tests |
| Secret leak in logs or audit | Structured field allowlists and secret-safe errors | Seed unmistakable dummy credentials, verify absence in every output sink |
| Resource exhaustion | Body/page limits, bounded concurrency, deadlines, queue quotas | Slow client/provider, giant response, queue flooding and cancellation tests |
| Accidental destructive/spend action | Distinct capabilities, reviewed intent, fresh auth for high impact | Confirmed target/impact cannot change between review and execution |

## Identity and bootstrap

Use one explicitly configured OIDC issuer per installation initially. Authorization code flow with PKCE, state and nonce; validate issuer, audience, signatures, token expiry, and exact registered callback. Bind identity to `(issuer, subject)`. Provider OAuth credentials and Sama login are different concepts and never interchangeable.

The installation administrator configures the initial owner subject through a local bootstrap command or one-use secret file; no “first visitor becomes admin” endpoint. Additional users need an explicit invitation or membership grant. Account linking needs an authenticated owner-assisted flow; matching email alone never grants access. Prevent removing the last owner without transferring ownership.

Use random opaque session tokens with 256 bits of entropy. Store only a token digest server-side; rotate at login and privilege elevation. Production cookie: `__Host-sama_session`, `Secure`, `HttpOnly`, `SameSite=Lax`, `Path=/`, no Domain. Target expiry: 30-minute idle and 12-hour absolute; policies configurable. High-impact actions require an OIDC authentication age under five minutes with provider-supported reauthentication/MFA policy. Logout deletes the session and clears caches; membership changes are effective at the next request and before job dispatch.

The development identity fixture must use a separate explicit mode, localhost binding, no real credentials, and synthetic accounts. There is no production “disable authentication” flag once provider connections exist.

## Authorization

Roles define ceilings; connection grants narrow access. All checks happen in application services and before provider dispatch. A UI action list is guidance, not authorization.

| Action class | Viewer | Operator | Admin | Owner |
| ----------------------------------------------------- | ------ | -------------------------- | -------------------------- | -------------------------- |
| Read granted resources and safe operation history | Yes | Yes | Yes | Yes |
| Trigger bounded refresh on granted connections | No | Yes | Yes | Yes |
| Low/disruptive operations on granted connections | No | Explicit action grant | Explicit action grant | Explicit action grant |
| Create/resize/rebuild/delete, security policy changes | No | Separate high-impact grant | Separate high-impact grant | Separate high-impact grant |
| Add/rotate/disable workspace connections | No | No | Yes | Yes |
| Manage viewer/operator membership and grants | No | No | Within own ceiling | Yes |
| Assign admins/owners, delete workspace | No | No | No | Yes |
| Read stored plaintext provider credentials | Never | Never | Never | Never |

Connection management is a powerful role because a broad provider credential can expand available access. Validate and audit changes. New grants are deny-by-default; the owner can explicitly grant itself operations, recorded in audit. No role overrides provider permission limits. Resource IDs outside an authorized scope return 404; authenticated denial within a known scope returns 403. Background jobs run under the original actor’s current authority, not a global administrator identity.

## Credential lifecycle

Credential entry is write-only over TLS. A separate connection test is only a preview: connection creation and rotation revalidate the exact credential submitted in their own request, derive account identity and capabilities from that provider response, and persist nothing when validation fails. Browser-supplied validation metadata is never authoritative. Encrypt the successfully validated allowlisted typed credential payload with AES-256-GCM using a fresh random per-version data key and nonce. Wrap that data key with the active master key using a separate nonce. Bind both ciphertext and wrapping operation to format version, workspace ID, connection ID, provider family, and credential version as authenticated additional data. Use Go’s reviewed cryptographic primitives; do not invent a cipher or share nonces.

Store ciphertext, wrapped key, both nonces, key ID, and format version in PostgreSQL. Keep the master keyring in a mode-0600 mounted secret file outside the database and backups. KMS wrapping can be a later implementation of the key-provider interface. Never generate a new replacement key silently when an existing key is missing. Startup/readiness fails closed when a required key is absent; liveness can remain healthy.

Decrypt only for a short-lived authorized adapter call. Avoid unnecessary byte/string copies and never attach secrets to errors; Go cannot guarantee perfect memory erasure. Keep token refresh state scoped to one connection/version; serialize concurrent refresh and renew before expiry with jitter. Persist refresh credentials only when necessary and encrypted. A refresh failure never falls back to another connection or ambient host credentials.

Master-key rotation: load old+new key IDs → use new key for writes → rewrap existing data keys in bounded transactions → verify all referenced rows → retain old keys until backup retention permits retirement. Provider credential rotation changes the active credential version and invalidates caches. Disable blocks new dispatch, while already-submitted actions remain visible for reconciliation. If compromise is suspected, revoke at the provider; deleting Sama’s copy alone does not revoke the credential.

## Browser and request boundaries

Serve SPA and API from the same origin. Require JSON content type and a session-bound CSRF header for state-changing endpoints, validate Origin against one configured public origin, and reject missing/untrusted origins on browser mutation routes. SameSite cookies supplement these checks. OIDC callback is a narrow exception protected by its own state/nonce flow. Do not enable wildcard credentialed CORS. [OWASP CSRF guidance](https://cheatsheetseries.owasp.org/cheatsheets/Cross-Site_Request_Forgery_Prevention_Cheat_Sheet.html)

React text rendering is the default for all provider data; no raw HTML errors, embedded remote documents, or arbitrary scriptable URLs. CSP disallows inline scripts, third-party scripts, frames, objects, and base tag changes. No provider token in localStorage, sessionStorage, URLs, analytics, client error tracking, or operation payloads. Public build-time environment values are never a secret channel. No third-party analytics by default.

Target JSON request cap: 1 MiB; header cap: 32 KiB; outbound metadata response cap: 8 MiB, adjusted down per endpoint when possible. File transfer requires a separate streaming design. Reject unknown fields on mutation inputs and validate every enumeration and identifier. Only trust forwarding headers from a configured proxy network; construct callbacks from a configured origin, never arbitrary Host/X-Forwarded-Host.

## Outbound security

Adapters use compiled approved provider endpoint profiles and HTTPS verification. Do not accept a free-form API URL during normal onboarding. Disable automatic redirects on credential-bearing requests; if an adapter needs a redirect, validate the destination and strip credentials on authority changes. Never follow upstream pagination URLs without validating scheme, host, port, and path family; prefer extracted opaque cursor values.

Resolve and validate all IPv4/IPv6 addresses at connection time, not only when the URL is saved. Reject loopback, private, link-local, unspecified, multicast, and metadata ranges for public provider profiles; tie dialing to the validated addresses to resist DNS rebinding. Prohibit userinfo and unapproved ports. A future local S3/MinIO profile must be installation-admin configured with a narrow private-network allowlist; it cannot weaken public-provider defaults. [OWASP SSRF guidance](https://cheatsheetseries.owasp.org/cheatsheets/Server_Side_Request_Forgery_Prevention_Cheat_Sheet.html)

## Audit and response

Audit authentication changes, membership and grant changes, credential creation/rotation/disabling, review acceptance, operation transitions and manual resolution. Store selected target IDs and changed field names, not raw request/response bodies. High-volume resource reads use request metrics/access logging with no secrets; sensitive data reads get dedicated audit events.

The runtime DB role cannot update/delete audit rows. This is append-only for the application, **not** tamper-proof against a DB administrator. Later external append-only export can strengthen forensic integrity. Secret incident runbook: disable affected connection/actions, revoke provider credentials, invalidate sessions if needed, inspect correlated audit events, rotate encryption keys if compromised, restore trusted code, then reconcile affected provider resources before re-enabling writes.
