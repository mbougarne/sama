# Frontend architecture

## Stack and boundaries

The frontend is the administrative **backoffice** for cloud operators. The Go API supplies the backend-for-frontend role within the selected modular monolith; no separate Node runtime service is introduced.

React + TypeScript with Webpack is the fixed frontend stack. Keep its package manifest, lockfile, TypeScript configuration, Webpack configuration, source, assets, and future tests inside `frontend/`. No root npm workspace is needed for a single web frontend. Exact initial versions now live in its manifest and lockfile.

Use React Router for navigation and TanStack Query for server state. Use React state, reducers, and narrowly scoped context for client interaction state. No separate Redux or Zustand store is selected. Webpack should handle production chunk hashes, separate CSS assets, and a development API proxy. Node is frontend build/development tooling, not a production server requirement.

Target feature folders under `frontend/src/features/`: `identity`, `workspaces`, `connections`, `resources`, `operations`, `activity`, `settings`. Each owns its components, API helpers, and feature-specific types. Shared `components` contains reusable accessible UI; shared `api` contains transport and generated contract types. Avoid a monolithic provider dashboard and a global store mirroring server data.

## Interface direction

The selected navigation and workflows are described in [product scope](../product.md). Review those journeys before producing screens or UI code.

Prefer local system fonts and no third-party trackers. Preserve the Arabic name as Unicode with appropriate text direction. English is the proposed initial interface language; Arabic/RTL localization remains a separate scope decision.

## State management

State has explicit owners. The following boundaries govern data lifecycle and security behavior.

| State | Owner | Examples and lifetime |
| --- | --- | --- |
| Server data | TanStack Query | Resource observations, connection metadata, capabilities, operations, activity; in-memory query cache |
| Navigation and shareable filters | React Router URL parameters | Workspace UUID, resource UUID, type/region filters, cursor, active detail tab; never secrets |
| Local interaction | React `useState` | Open panel, input text, selected row; ends when its view unmounts |
| Multi-step forms | Feature-local `useReducer` | Connection onboarding and action review transitions; discard on logout/scope change |
| Shared UI concerns | Narrow React context | Theme and locale selection; active workspace comes from the validated route, not a second mutable store |
| Authentication and permissions | Server authority, queried through TanStack Query | Session/member/capability metadata is for rendering; every backend request and job must authorize independently |

Do not copy fetched inventory, operations, or permissions into context, a reducer, or another global store. Components read query results and derive their view. A small edited form can hold a deliberate draft of selected fields, with an explicit reset/revalidation rule; it must not become a second resource database.

React's reducer/context combination supports organizing related interaction state. TanStack Query owns fetched-data caching and revalidation; configure its defaults explicitly instead of assuming its built-in staleness/retry behavior matches cloud operations. [React guidance](https://react.dev/learn/scaling-up-with-reducer-and-context), [TanStack Query defaults](https://tanstack.com/query/latest/docs/framework/react/guides/important-defaults)

### Query identity and freshness

Keys contain the authenticated user UUID, workspace UUID, feature name, connection/resource UUID where applicable, and normalized filters/cursor. Provider native IDs alone are insufficient. For example, an inventory key identifies user + workspace + resources + connection + type + region + cursor. A key names data; it does not grant permission.

Initial settings to implement and measure:

| Data | Freshness and refresh rule |
| --- | --- |
| Inventory and resource details | 30-second query stale time; refetch stale data on mount, window focus, or network reconnect; reads hit Sama's stored observations |
| Connection metadata and capabilities | Stale immediately; refresh on view entry and after connection/grant changes; backend still rechecks every action |
| Running operations and syncs | Poll the visible active view every three seconds; pause while hidden/offline; stop on success, failure, cancellation, or unresolved `unknown` |
| Unknown operation | Show last evidence and an explicit reconciliation action; no automatic mutation retry |
| Activity/history | 30-second stale time, refresh on entry/focus or related completion; paginated |
| Inactive queries | Five-minute garbage-collection limit, with earlier explicit removal on logout or scope change |

Browser cache age is different from provider observation age. Always display the backend's `observed_at` and sync status. A successful HTTP refresh cannot turn an old provider observation into fresh cloud state. UI polling reads Sama; it does not directly trigger a provider API call for each browser tab.

### Mutations and cache updates

Use one API transport for same-origin cookies, CSRF headers, UUID serialization, request correlation, AbortController, and bounded deadlines. Mutation retries are disabled. Read requests get at most one retry for transient network/5xx failures within their deadline, with jitter; do not retry 401, 403, validation, or conflict errors. Respect rate-limit hints instead of automatic retry loops.

A mutation returns a durable operation reference. Track that operation and invalidate affected resource, connection, and activity keys as its state changes. Do not optimistically mark cloud state successful. Only harmless local metadata can use reversible optimistic updates with rollback and revalidation.

Keep the operation's idempotency key in the submitting feature's memory while acceptance is unresolved. A dropped confirmation response is recovered with the same key or the operation history, never an automatic new intent. Disabling a button reduces accidental clicks; server idempotency provides the actual protection. A multi-step reducer explicitly models draft, reviewing, reviewed, submitting, accepted, and error states. Editing the target or parameters invalidates its review.

### Logout, scope changes, and persistence

On logout/session expiry: stop polling, abort in-flight fetches, remove sensitive query data, clear form drafts/CSRF memory, and create a fresh query-client scope before the next authenticated user. On workspace change: cancel/remove the previous workspace's queries and drafts before rendering the new one. Check membership before requesting its data. A late response must remain tied to its old scope and never populate the current view.

On permission loss, discard affected resource/capability caches and reset forbidden views. Treat 401 as a session transition, not an ordinary retryable query error. On 403/404, clear the affected view and refresh membership/capabilities without leaking another scope's data. The session cookie stays HttpOnly; no browser state store gets its value.

Do not persist the query cache in localStorage, sessionStorage, IndexedDB, or a service worker. Provider credentials exist only in the short-lived input state required for submission and are cleared after use. Persist only allowlisted cosmetic preferences such as theme and locale, with an installation/user-specific key; never resource lists, account credentials, session/CSRF tokens, or action reviews.

### Future evidence

Verify one owner per state category, no duplicate server-data store, bounded polling, mutation retries disabled, backend freshness distinct from browser freshness, and cache removal after logout/permission loss. Include late-response workspace switching, a second user signing into the same browser, an interrupted confirmation request, and form review invalidation. No frontend checks or code are introduced during architecture stabilization.

## Interaction design

Use provider/account/region identity consistently near each resource title and action review. Keep common details scannable; put native options in labeled advanced sections. Separate lack of permission from unsupported integration and invalid current state. Preserve the user’s form input after recoverable validation errors, except secret fields where retaining values requires careful handling.

Credential forms show provider-specific fields from reviewed, typed metadata, not arbitrary JSON schema or HTML sent by a plugin. Secrets are masked by default, never filled from a read endpoint, and cleared after save/unmount. Native password manager/autocomplete behavior should be tested rather than accidentally disabled globally.

Dangerous flows show exact target and impact, use keyboard-accessible dialogs with focus management, and require risk-appropriate confirmation. A failed request after pressing Confirm may still have been accepted; recover using the same idempotency key or look up the operation. Do not invite another click with a new key before establishing the previous result.

## Accessibility and performance

Target WCAG 2.2 AA: semantic headings/landmarks, programmatic labels, status text beyond color, visible focus, keyboard operation, accessible validation, and dialog focus restoration. Test mobile at 390 px and desktop at 1440 px; also check 200% zoom and reduced motion before the first authenticated release. Automated scans supplement manual keyboard checks.

Use route-based code splitting once multiple features exist, local fonts, stable layout, and no provider SDKs in the browser. Initial budgets: total route entry under 350 kB uncompressed and individual asset under 250 kB, to be enforced by the future Webpack configuration; target under 150 kB compressed JS for the first inventory route. Add a measured browser performance budget once realistic inventory data exists. Use bounded pages before virtualization, and virtualization only when necessary.

Production CSS is extracted to files so the CSP can disallow inline styles/scripts. Choose the CSS pipeline during implementation, evaluating Webpack’s native CSS and explicit extraction against the required CSP. [Webpack production guide](https://webpack.js.org/guides/production/), [CSS extraction](https://webpack.js.org/plugins/mini-css-extract-plugin/)

## Frontend acceptance criteria

Future production browser checks should cover real API loading, search/filter/empty states, backend failure recovery, mobile overflow, and browser console errors. Future gates add authenticated user journeys, revoked sessions, cross-workspace cache clearing, interrupted mutation responses, unknown operations, stale preconditions, and keyboard dialogs. Avoid snapshot-only tests that merely freeze markup. The release UI must be tested through the Go static server, not only Webpack’s development server.
