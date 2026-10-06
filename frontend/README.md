# Sama backoffice

This is the administrative interface for people managing their cloud services. It is a React browser application built with TypeScript and Webpack. The Go backend owns the browser-facing API, authentication/authorization and provider credentials. Node is development/build tooling; no separate Node BFF server is introduced.

The interface checks the current session before showing the overview and shared layout, using React Router and TanStack Query. It contains no fake resource totals or cloud actions. Transport reads have at most one transient retry; query-library and mutation retries remain disabled.

Use Node **24.20.0**, pinned in `.nvmrc` and `package.json`. Use pnpm **10.6.5**, declared in `package.json`; `.npmrc` is also pnpm configuration. From this directory:

```sh
nvm install
nvm use
pnpm install --frozen-lockfile --ignore-scripts
pnpm run dev
```

The development server binds to `http://127.0.0.1:3000`. `/api`, `/auth` and `/health` proxy to the Go server at `127.0.0.1:8080`. Start the backend separately when using those routes. Webpack's development server is not a production deployment server.

```sh
pnpm run format       # Explicitly format frontend files
pnpm run api:generate # Regenerate API types from ../backend/api/openapi.yaml
pnpm run api:check    # Verify checked-in API types match the source contract
pnpm test             # Jest component tests, once
pnpm run test:watch   # Jest during development
pnpm run check        # Formatting, API drift, ESLint, TypeScript, Jest and production build
pnpm run build        # Production assets in ignored dist/
```

Formatting never targets the repository's local `agents/` records. Dependencies and the lockfile belong here; there is no root package workspace. Direct package versions are exact, and `pnpm install --frozen-lockfile` installs the lockfile. See [Contributing](../CONTRIBUTING.md#pre-commit) for commit checks.

The backend owns the source contract at `../backend/api/openapi.yaml`. Generated types are checked in under `src/api/generated/`; edit the OpenAPI document, run `pnpm run api:generate`, and include both source and generated output in the same change. CI runs the non-mutating `api:check` drift comparison.

Jest runs `tests/**/*.test.ts(x)` in jsdom with React Testing Library and user-event. Tests use Jest's ES-module mode to load React Router's ESM distribution. The test commands include Node's required `--experimental-vm-modules` flag, which emits an experimental-feature warning. No external Watchman service is required. Babel strips TypeScript while preserving ES modules; `pnpm run typecheck` separately checks application and test types. Webpack owns the production build. Component tests cover navigation and keyboard entry; jsdom tests do not substitute for browser layout or accessibility review.

For production-like local smoke checks, build with `pnpm run build`, then set
`SAMA_ASSET_DIR` to this directory's `dist` when starting Go on an isolated
loopback port. Go serves the browser and API from one origin with strict CSP;
Webpack remains a separate build step. No deployed environment is implied.

`src/api/client.ts` is the browser transport. It sends same-origin cookies and
reads the CSRF cookie only when sending a mutation; it never persists tokens.
Each request shares one bounded deadline across at most one transient read retry
with jitter. Mutations and access/validation/conflict/rate-limit failures are not
retried. Caller cancellation remains distinguishable from deadline expiry.
Errors expose local safe messages, allowlisted codes/field errors, request IDs and
bounded retry hints; raw server/provider messages are discarded. A 401 emits the
session-expiry event for the authentication boundary. Generated types back the
current-user and workspace-page helpers; routes cannot select another origin.

The backoffice checks `/api/v1/me` before rendering private content and during
session revalidation. Login uses `/auth/login`; sign-out calls the guarded logout
route. Any API 401 hides private views and clears local queries without a retry
loop. Failed sign-out remains visibly unconfirmed. The development proxy includes
`/auth`, and local identity still requires the backend's explicit loopback mode.

When using the local Webpack proxy with synthetic OIDC, configure the backend
public origin as `http://127.0.0.1:3000` so Origin checks and callbacks match the
browser origin. Production continues to use one HTTPS origin served through Go.

Authenticated feature data lives in a fresh `QueryScope` per user and workspace.
Scope replacement cancels/removes the old client's queries and remounts local
form drafts. Use `scopedKey` with user/workspace UUIDs, feature, and normalized
resource filters; array filters are treated as sets. No query cache is persisted
in browser storage. Membership checks stay outside the workspace data scope and
must finish before it is rendered; keys and cached role hints are not authority.

Workspace selection lives at `/workspaces/{uuid}/{section}`. The authorized,
paginated workspace endpoint supplies names and roles; a single workspace skips
the picker. URL changes and background access revalidation hide the old view;
removed/unknown scopes show no workspace content. The six navigation sections
are available as routes, with unimplemented product features labelled explicitly.
Keyboard tests cover selection/navigation; real 390 px/zoom reflow still needs
browser acceptance before release.
