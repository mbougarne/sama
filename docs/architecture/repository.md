# Repository structure

## Ownership and layout

Backend and frontend are separate projects with their own dependencies, configuration, tests and build output. The following is the architectural expansion map, not a checklist of folders to create. Add modules with the features that need them. Global project documents stay at the root; detailed design and shared agent rules live under `docs/`.

The ignored local `agents/` store is defined by the [record format](../agents/records.md). Shared collaboration tools, hook helpers and their tests live under `scripts/`; GitHub workflows under `.github/workflows/` invoke each project's checks. The tracked hook entry point lives in `.githooks/`. These shared entry points coordinate tooling without taking ownership of backend/frontend logic.

```text
sama/
├── README.md, LICENSE, AGENTS.md, …
├── docs/
├── backend/
│   ├── README.md
│   ├── go.mod
│   ├── go.sum
│   ├── cmd/
│   │   └── sama/
│   ├── internal/
│   │   ├── identity/
│   │   ├── workspace/
│   │   ├── connection/
│   │   ├── inventory/
│   │   ├── operation/
│   │   ├── audit/
│   │   ├── provider/
│   │   ├── job/
│   │   ├── platform/
│   │   └── httpapi/
│   ├── api/
│   │   └── openapi.yaml
│   ├── migrations/
│   └── tests/
│       └── integration/
├── frontend/
│   ├── README.md
│   ├── package.json
│   ├── pnpm-lock.yaml
│   ├── tsconfig.json
│   ├── webpack.config.cjs
│   ├── public/
│   ├── src/
│   │   ├── app/
│   │   ├── features/
│   │   ├── components/
│   │   └── api/
│   │       └── generated/
│   └── tests/
│       └── e2e/
└── deploy/                 Shared release assembly, when needed
```

Create each internal module with the feature that needs it; do not scaffold every package at once. Go unit tests live beside their packages inside `backend/`. Backend integration fixtures belong there too. Frontend component and browser checks belong under `frontend/`. Generated build output and installed dependencies remain local to their owning project and outside version control.

## Ownership rules

| Concern | Owner |
| --- | --- |
| Go source, Go dependencies, backend configuration and build tooling | `backend/` |
| Database migrations, persistence, provider adapters, credentials and workers | `backend/` |
| Source HTTP API contract | `backend/api/` |
| React source, assets, npm dependencies, TypeScript and Webpack configuration | `frontend/` |
| Generated TypeScript API types and browser API client | `frontend/src/api/` |
| Backend tests and fixtures | `backend/` |
| Frontend and browser tests and fixtures | `frontend/` |
| Application-wide deployment assembly | `deploy/`, once implementation needs it |
| Architecture, research and design decisions | `docs/` |
| License, project introduction and contributor guidance | Repository root |

A single frontend does not need a root npm workspace. Backend tooling must not depend on a root Go module. Future convenience commands may coordinate both projects, but they should call each project’s own tooling rather than become the only way to build it.

## Integration without mixed source trees

The backend owns the HTTP API contract. A future generation step reads that contract and writes frontend types into the frontend directory. Frontend code communicates with Go over HTTP; it does not import backend source or provider SDKs.

During development, the frontend’s Webpack server can proxy API calls to the Go server. Each runs from its own folder, with its own configuration. Commands and addresses belong in the [backend](../../backend/README.md) and [frontend](../../frontend/README.md) guides.

For deployment, source separation and runtime packaging are separate decisions. The selected initial release combines an independently built Go binary and frontend assets in one image for easy self-hosting. This preserves folder separation. The [deployment architecture](deployment.md) describes assembly; the choice does not require application source at the repository root.
