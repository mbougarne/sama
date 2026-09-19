# Sama review checks

Use this file as a relevance map, not a requirement to run every command. The workflow supplies a current check-run snapshot; report it accurately and do not execute repository code during review.

## Repository boundaries

- Follow `AGENTS.md` and the trusted `docs/agents/` rules from the base revision.
- Preserve the fixed product and stack constraints. Do not propose payments, a substitute framework, or unrelated roadmap expansion.
- Keep frontend, backend, provider, and operational responsibilities explicit; check shared contract changes across every affected boundary.
- Treat documentation and collaboration-ledger consistency as repository correctness where the trusted rules require it.

## Backend

- Use the Go version declared by the module. The canonical backend check is `sh scripts/check.sh`.
- Relevant coverage includes formatting, vet, race-enabled tests, and builds.
- Examine input validation, authorization, persistence, concurrency, cancellation, resource ownership, provider abstraction boundaries, error behavior, and compatibility.
- For provider-backed behavior, distinguish deterministic local tests from live provider or production acceptance.

## Frontend

- Use the Node version in `frontend/.nvmrc` and the package manager pinned by the frontend project.
- Relevant checks include Prettier, ESLint, TypeScript, Jest, and a production Webpack build.
- Review component state, async races and cleanup, accessibility, keyboard/focus behavior, API contract handling, error states, and responsive behavior when affected.

## Collaboration and documentation

- Relevant CI includes collaboration unit tests and `scripts/collab.py lint` in addition to product checks.
- When code changes alter supported behavior, verify that architecture, validation, research, and project records stay consistent where the repository rules require updates.
- Do not demand duplicate documentation or speculative records that the trusted governance rules do not require.

## Evidence standard

- A failing command or check is not itself a code defect; connect it to an introduced behavior and location.
- Passing unit tests, lint, or builds do not establish live providers, browsers, operating systems, accessibility technology, or production fixtures.
- Do not recommend weakening the rules, types, tests, checks, or pinned toolchain merely to obtain a green result.
