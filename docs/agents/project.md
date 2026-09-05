# Sama project rules

Sama (سماء, sky) is a self-hosted cloud backoffice. Read the root `README.md`, `docs/README.md`, and the relevant architecture documents before making changes.

## Scope and documentation ownership

- Work within the explicit user task. Design acceptance is not blanket authorization for unrelated implementation or external actions. Create feature folders with their first agreed implementation, not speculatively.
- Keep architecture stable. Amend architecture or ADRs only during an explicit design revisit or agreed design change. Do not add conversation summaries, phase banners, implementation progress or version-pin updates to architecture documents.
- Keep setup and commands in `CONTRIBUTING.md` and the owning backend/frontend README. Keep task progress, evidence and handoffs in ignored local `agents/` records.
- The core stack is fixed: Go backend, React + TypeScript frontend, Webpack, PostgreSQL, one repository. Do not propose replacing these languages, frameworks, or bundler. Security/version updates and improvements within this stack remain in scope.
- Plan all backend code and tooling under `backend/` and all frontend code and tooling under `frontend/`. Global project documents belong at the root; detailed architecture belongs under `docs/`.
- Preserve useful provider research and cite its sources. Do not use ambient provider credentials or make cloud changes.
- Sama manages provider products, never payments. Do not design payment collection/processing, checkout, payment-method storage, invoice settlement, account top-ups, refunds, or resale billing. Resource creation may incur charges billed directly by the provider; it must not introduce payment handling into Sama.
- For documentation edits, check consistency and local links; do not introduce a test/build system just to validate documents.
- Do not assume a GitHub owner, deployment environment, or publication request. The temporary local module path is sama/backend until hosting identity is selected.

## Accepted decisions

- Use PostgreSQL for durable state and jobs; start with bounded in-process caching, without Redis. A shared cache would require measured need and an explicit architecture amendment.
- Use UUIDv7 for Sama entity keys and public API identifiers; `id` is a field name, not an integer type. Keep provider IDs opaque and authentication tokens independently random.
- Frontend state ownership: TanStack Query for server data, React Router for URL state, local React state/reducers for interaction, and narrowly scoped context for shared UI concerns. Do not duplicate server data in a global store.
- Use the standard MIT license. Keep sandbox-before-production guidance outside the license as operational advice, not an extra condition of use.
- Follow `CODE_OF_CONDUCT.md`: technical scope, specific behavioral feedback, no character/upbringing/family labels, and no stack-replacement debates. Conduct rules do not restrict licensed software use.

## Design principles

Keep generic workflows and transport separate from provider-specific semantics. Distinguish what a provider offers, what a future adapter implements, and what the user is authorized to do. A timeout is not proof that a cloud action failed; a database lease does not fence a remote API.

See `docs/architecture/repository.md` for the selected repository structure and `docs/roadmap.md` for the implementation sequence.
