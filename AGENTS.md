# Working on Sama

Sama (سماء, sky) is currently in the **architecture-only stabilization phase**. Read `README.md`, `docs/README.md`, and the relevant architecture documents before making changes.

## Current scope

- Work on documentation, research, diagrams, and architecture decisions only.
- Do not create application code, tests, dependency manifests, generated clients, build tooling, CI workflows, containers, or runnable previews until the user explicitly asks to begin implementation.
- Distinguish accepted design from implemented behavior. The user accepted the existing ADR decisions on 2026-09-05, subject to the refinements recorded in `docs/decisions/README.md`. Keep genuinely new proposals labeled; acceptance of architecture does not authorize coding.
- The core stack is fixed: Go backend, React + TypeScript frontend, Webpack, PostgreSQL, one repository. Do not propose replacing these languages, frameworks, or bundler. Security/version updates and improvements within this stack remain in scope.
- Plan all backend code and tooling under `backend/` and all frontend code and tooling under `frontend/`. Global project documents belong at the root; detailed architecture belongs under `docs/`.
- The selected future folder tree is documentation only. Do not create empty implementation folders during this phase.
- Preserve useful provider research and cite its sources. Do not use ambient provider credentials or make cloud changes.
- Sama manages provider products, never payments. Do not design payment collection/processing, checkout, payment-method storage, invoice settlement, account top-ups, refunds, or resale billing. Resource creation may incur charges billed directly by the provider; it must not introduce payment handling into Sama.
- For documentation edits, check consistency and local links; do not introduce a test/build system just to validate documents.
- Do not assume a GitHub owner, module path, deployment environment, or publication request.

## Stabilization decisions

- Use PostgreSQL for durable state and jobs; start with bounded in-process caching, without Redis. A shared cache would require measured need and an explicit architecture amendment.
- Use UUIDv7 for Sama entity keys and public API identifiers; `id` is a field name, not an integer type. Keep provider IDs opaque and authentication tokens independently random.
- Frontend state ownership: TanStack Query for server data, React Router for URL state, local React state/reducers for interaction, and narrowly scoped context for shared UI concerns. Do not duplicate server data in a global store.
- Use the standard MIT license. Keep sandbox-before-production guidance outside the license as operational advice, not an extra condition of use.
- Follow `CODE_OF_CONDUCT.md`: technical scope, specific behavioral feedback, no character/upbringing/family labels, and no stack-replacement debates. Conduct rules do not restrict licensed software use.

## Design principles

Keep generic workflows and transport separate from provider-specific semantics. Distinguish what a provider offers, what a future adapter implements, and what the user is authorized to do. A timeout is not proof that a cloud action failed; a database lease does not fence a remote API.

See `docs/architecture/repository.md` for the selected repository structure and `docs/roadmap.md` for the review phase and future implementation sequence.
