# Repository structure

## Current phase

Sama contains architecture and project documentation only. There is no application to run, no dependency setup, and no test or build system. Read and refine the design before creating implementation folders.

Current structure:

```text
sama/
├── README.md
├── LICENSE
├── AGENTS.md
├── CONTRIBUTING.md
├── SECURITY.md
├── CODE_OF_CONDUCT.md
└── docs/
    ├── README.md
    ├── product.md
    ├── providers.md
    ├── roadmap.md
    ├── architecture/
    │   ├── README.md
    │   ├── repository.md
    │   ├── data.md
    │   ├── api.md
    │   ├── security.md
    │   ├── operations.md
    │   ├── frontend.md
    │   └── deployment.md
    └── decisions/
        └── README.md
```

Git metadata is omitted from the tree. Global project documents stay at the root. Detailed design belongs in `docs/`.

## Future implementation layout

The following is a proposed directory map, not a set of folders to create now. Backend/frontend separation is a user requirement; internal details can be refined during architecture review.

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
│   ├── package-lock.json
│   ├── tsconfig.json
│   ├── webpack.config.js
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

Create each internal module with the feature that needs it; do not scaffold every package at once. Go unit tests would live beside their packages inside `backend/`. Backend integration fixtures belong there too. Frontend component and browser checks belong under `frontend/`. Generated build output and installed dependencies remain local to their owning project and outside version control.

## Ownership rules

| Concern | Future owner |
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

During development, the frontend’s Webpack server can proxy API calls to the Go server. Each runs from its own folder, with its own configuration. There are no commands or port assignments to configure in the current phase.

For deployment, source separation and runtime packaging are separate decisions. A release can combine an independently built Go binary and frontend assets into one image for easy self-hosting, or serve frontend assets separately behind the same origin. Both preserve the required folder separation. The [deployment proposal](deployment.md) describes the combined-image option; packaging remains open for review.
