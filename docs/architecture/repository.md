# Repository structure

## Current phase

Sama contains architecture/project documentation and Python AI collaboration tooling. There is no application to run or application dependency/build setup. Collaboration checks live separately under `scripts/tests/`. The folder ownership is selected; implementation folders will be created only after an explicit request to begin coding.

Current structure:

```text
sama/
├── README.md
├── LICENSE
├── .gitignore              Excludes local agents/ records
├── AGENTS.md
├── CONTRIBUTING.md
├── SECURITY.md
├── CODE_OF_CONDUCT.md
├── scripts/                   Collaboration CLI, engine, templates and tests
└── docs/
    ├── README.md
    ├── product.md
    ├── providers.md
    ├── roadmap.md
    ├── agents/
    │   ├── README.md
    │   ├── project.md
    │   ├── records.md
    │   ├── research.md
    │   ├── rules/              All four shared rule documents
    │   └── validation.md
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

Git metadata and the ignored local `agents/` record store are omitted from the tree. The [agent record format](../agents/records.md) defines its conversations, inputs, researches, commands, and history index. Shared collaboration rules live under `docs/agents/`; collaboration tools and their checks live under `scripts/`, separately from policy and future product tooling. Global project documents stay at the root. Detailed design belongs in `docs/`.

## Future implementation layout

The following is the selected future directory map, not a set of folders to create now. Backend/frontend separation is a user requirement; add each internal module only with its first feature.

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

For deployment, source separation and runtime packaging are separate decisions. The selected initial release combines an independently built Go binary and frontend assets in one image for easy self-hosting. This preserves folder separation. The [deployment architecture](deployment.md) describes assembly; the choice does not require application source at the repository root.
