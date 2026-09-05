# Frontend architecture

## Stack and boundaries

React + TypeScript with Webpack is the proposed frontend stack. Keep its package manifest, lockfile, TypeScript configuration, Webpack configuration, source, assets, and future tests inside `frontend/`. No root npm workspace is needed for a single web frontend. Select exact versions when implementation begins.

Consider React Router for authenticated navigation and TanStack Query for server state; introduce additional state libraries only for a demonstrated need. Webpack should handle production chunk hashes, separate CSS assets, and a development API proxy. Node is frontend build/development tooling, not a production server requirement.

Target feature folders under `frontend/src/features/`: `identity`, `workspaces`, `connections`, `resources`, `operations`, `activity`, `settings`. Each owns its components, API helpers, and feature-specific types. Shared `components` contains reusable accessible UI; shared `api` contains transport and generated contract types. Avoid a monolithic provider dashboard and a global store mirroring server data.

## Interface direction

There is no implemented interface or runnable preview in this phase. The proposed navigation and workflows are described in [product scope](../product.md). Review those journeys before producing screens or UI code.

Prefer local system fonts and no third-party trackers. Preserve the Arabic name as Unicode with appropriate text direction. English is the proposed initial interface language; Arabic/RTL localization remains a separate scope decision.

## State and data access

URL state owns selected workspace, resource filters, stable pagination, and selected resource where deep linking matters. Local component state owns form drafts and UI expansion. Server state belongs in query caches with keys containing user/workspace/connection/resource identity. Clear all sensitive caches on logout, membership loss, and workspace change; cancel old in-flight requests so another workspace’s result cannot flash in the new view.

Generate DTOs from OpenAPI and keep handwritten UI view models separate. Browser fetch uses same-origin credentials and AbortController. Target transport adds CSRF, request IDs, bounded timeout, and stable error mapping; never globally retry mutations. Start with polling active operations every 2–5 seconds with jitter, stop/pause on terminal state or hidden tab, and use server-provided retry hints. Add SSE only if measured polling load warrants it, with authorization on reconnect and bounded event replay. WebSockets are not required.

Inventory list reads come from Sama’s local observations. Show “last updated” and stale/failed scope indicators. Refresh queues work and displays its progress; it cannot make data appear fresh before the provider scan completes. Optimistic updates are limited to harmless local metadata. Do not optimistically show a cloud mutation as completed.

## Interaction design

Use provider/account/region identity consistently near each resource title and action review. Keep common details scannable; put native options in labeled advanced sections. Separate lack of permission from unsupported integration and invalid current state. Preserve the user’s form input after recoverable validation errors, except secret fields where retaining values requires careful handling.

Credential forms show provider-specific fields from reviewed, typed metadata, not arbitrary JSON schema or HTML sent by a plugin. Secrets are masked by default, never filled from a read endpoint, and cleared after save/unmount. Native password manager/autocomplete behavior should be tested rather than accidentally disabled globally.

Dangerous flows show exact target and impact, use keyboard-accessible dialogs with focus management, and require risk-appropriate confirmation. A failed request after pressing Confirm may still have been accepted; recover using the same idempotency key or look up the operation. Do not invite another click with a new key before establishing the previous result.

## Accessibility and performance

Target WCAG 2.2 AA: semantic headings/landmarks, programmatic labels, status text beyond color, visible focus, keyboard operation, accessible validation, and dialog focus restoration. Test mobile at 390 px and desktop at 1440 px; also check 200% zoom and reduced motion before the first authenticated release. Automated scans supplement manual keyboard checks.

Use route-based code splitting once multiple features exist, local fonts, stable layout, and no provider SDKs in the browser. Initial budgets: total route entry under 350 kB uncompressed and individual asset under 250 kB, to be enforced by the future Webpack configuration; target under 150 kB compressed JS for the first inventory route. Add a measured browser performance budget once realistic inventory data exists. Use bounded pages before virtualization, and virtualization only when necessary.

Production CSS is extracted to files so the CSP can disallow inline styles/scripts. Choose the CSS pipeline during implementation, evaluating Webpack’s native CSS and explicit extraction against the required CSP. [Webpack production guide](https://webpack.js.org/guides/production/), [CSS extraction](https://webpack.js.org/plugins/mini-css-extract-plugin/)

## Proposed frontend acceptance criteria

Future production browser checks should cover real API loading, search/filter/empty states, backend failure recovery, mobile overflow, and browser console errors. Future gates add authenticated user journeys, revoked sessions, cross-workspace cache clearing, interrupted mutation responses, unknown operations, stale preconditions, and keyboard dialogs. Avoid snapshot-only tests that merely freeze markup. The release UI must be tested through the Go static server, not only Webpack’s development server.
