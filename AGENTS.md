# Working on Sama

Sama (سماء, sky) is currently in the **architecture-only review phase**. Read `README.md`, `docs/README.md`, and the relevant architecture documents before making changes.

## Current scope

- Work on documentation, research, diagrams, and architecture decisions only.
- Do not create application code, tests, dependency manifests, generated clients, build tooling, CI workflows, containers, or runnable previews until the user explicitly asks to begin implementation.
- Describe unimplemented behavior as a proposal or future requirement. Do not mark architectural proposals as accepted merely because they have been written.
- Keep the requested stack: Go backend, React frontend, Webpack bundler, one repository.
- Plan all backend code and tooling under `backend/` and all frontend code and tooling under `frontend/`. Global project documents belong at the root; detailed architecture belongs under `docs/`.
- The proposed folder tree is documentation only. Do not create empty implementation folders during this phase.
- Preserve useful provider research and cite its sources. Do not use ambient provider credentials or make cloud changes.
- Sama manages provider products, never payments. Do not design payment collection/processing, checkout, payment-method storage, invoice settlement, account top-ups, refunds, or resale billing. Resource creation may incur charges billed directly by the provider; it must not introduce payment handling into Sama.
- For documentation edits, check consistency and local links; do not introduce a test/build system just to validate documents.
- Do not assume a GitHub owner, module path, deployment environment, or publication request.

## Design principles

Keep generic workflows and transport separate from provider-specific semantics. Distinguish what a provider offers, what a future adapter implements, and what the user is authorized to do. A timeout is not proof that a cloud action failed; a database lease does not fence a remote API.

See `docs/architecture/repository.md` for the proposed repository structure and `docs/roadmap.md` for the review phase and future implementation sequence.
