# Contributing to Sama

Sama is in the architecture stabilization phase. Contributions currently focus on product scope, provider research, architectural tradeoffs, diagrams, and documentation clarity.

Read the [product scope](docs/product.md), [architecture](docs/architecture/README.md), and [repository structure](docs/architecture/repository.md). Explain the problem a change solves and its consequences for simplicity, security, and maintainability. Follow the accepted decisions; historical alternatives are not competing implementation options.

Keep new proposals distinguishable from accepted design. The Go, React + TypeScript, Webpack, and PostgreSQL stack is fixed; replacement debates are outside contribution scope. Follow the [conduct policy](CODE_OF_CONDUCT.md) and discuss specific technical issues rather than personal qualities. Cite official provider documentation for API claims, including the research date and unresolved questions. Check document links and consistency when changing paths or decisions.

Application code, application tests, dependencies, build tooling, and deployment configuration belong to a later phase, after the user explicitly starts implementation. Future Go work belongs under `backend/`; future React/Webpack work belongs under `frontend/`. Global project documents stay at the root.

AI-assisted work follows [AGENTS.md](AGENTS.md) and the [collaboration contract](docs/agents/README.md). Shared rules are versioned under `docs/agents/`; generated local records under `agents/` must not be committed or attached to contributions. Promote relevant sanitized findings into project documentation. The collaboration workflow is independent of future application linting.

All contributions are under MIT. Contributors retain ownership of their work and must have the right to contribute it. No CLA or automated sign-off gate is configured.

The repository is local. Public reporting contacts, maintainers, and hosting details will be established before public launch; no remote owner is assumed.

Commercial use, resale, private modifications, and building other products on Sama are permitted under the license, with its notice-retention condition. Contributor conduct governs this project's shared spaces; it does not add license restrictions. See the [MIT text](LICENSE).
