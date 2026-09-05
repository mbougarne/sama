# Sama backoffice

This is the administrative interface for people managing their cloud services. It is a React browser application built with TypeScript and Webpack. The Go backend owns the browser-facing API, authentication/authorization and provider credentials. Node is development/build tooling; no separate Node BFF server is introduced.

The interface contains an overview route, a not-found route, shared layout, React Router and TanStack Query providers. It contains no fake resource totals, account authentication or cloud actions. Server-data retry policy will be qualified when the first real query is implemented; the scaffold conservatively disables retries.

Use Node **24.20.0**, pinned in `.nvmrc` and `package.json`. Use pnpm **10.6.5**, declared in `package.json`; `.npmrc` is also pnpm configuration. From this directory:

```sh
nvm install
nvm use
pnpm install --frozen-lockfile --ignore-scripts
pnpm run dev
```

The development server binds to `http://127.0.0.1:3000`. `/api` and `/healthz` proxy to the Go server at `127.0.0.1:8080`. Start the backend separately when using those routes. Webpack's development server is not a production deployment server.

```sh
pnpm run format       # Explicitly format frontend files
pnpm test             # Jest component tests, once
pnpm run test:watch   # Jest during development
pnpm run check        # Formatting, ESLint, TypeScript, Jest and production build
pnpm run build        # Production assets in ignored dist/
```

Formatting never targets the repository's local `agents/` records. Dependencies and the lockfile belong here; there is no root package workspace. Direct package versions are exact, and `pnpm install --frozen-lockfile` installs the lockfile. See [Contributing](../CONTRIBUTING.md#pre-commit) for commit checks.

Jest runs `tests/**/*.test.ts(x)` in jsdom with React Testing Library and user-event. Tests use Jest's ES-module mode to load React Router's ESM distribution. The test commands include Node's required `--experimental-vm-modules` flag, which emits an experimental-feature warning. No external Watchman service is required. Babel strips TypeScript while preserving ES modules; `pnpm run typecheck` separately checks application and test types. Webpack owns the production build. Component tests cover navigation and keyboard entry; jsdom tests do not substitute for browser layout or accessibility review.
