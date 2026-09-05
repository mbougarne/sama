# Sama · سماء

**Your cloud, clearly.** Sama is an open source project for a self-hosted web application for managing cloud products through provider APIs. سماء means “sky” in Arabic.

## Current phase: architecture stabilization

This repository contains architecture documentation and local AI collaboration tooling; application implementation has not started. The owner accepted the main architecture decisions on 2026-09-05. The documentation now records that baseline and the refinements selected during this review; no architecture decision is a claim of implemented behavior. There is no application, application test suite, dependency installation, or deployment setup to run. The collaboration scripts have their own focused checks.

The fixed stack is Go, React + TypeScript, Webpack, and PostgreSQL, in the same repository with separate **`backend/`** and **`frontend/`** directories. These directories will be created when coding is explicitly requested. Global project documents stay at the root.

## Start reading

1. [Product scope](docs/product.md): what Sama should do and how it should feel to use.
2. [Architecture overview](docs/architecture/README.md): components, boundaries, and the selected technology choices.
3. [Repository structure](docs/architecture/repository.md): clear backend/frontend ownership and the future folder layout.
4. [Provider research](docs/providers.md): API findings and integration differences.
5. [Roadmap](docs/roadmap.md): architecture review first, then planned implementation phases.

The [documentation index](docs/README.md) links to the detailed data, API, security, operation lifecycle, frontend, deployment, and decision documents.

## Selected design

A Go application with separate internal modules, provider-specific adapters, a React + TypeScript interface, and PostgreSQL for durable state. Common workflows should be simple while preserving each provider’s native capabilities. Credentials remain on the backend; cloud changes have explicit permissions, review, and traceable outcomes.

**Sama manages products, never payments.** Users connect their own provider accounts and manage supported resources through APIs, including creating resources. Each provider bills the user directly and handles payment entirely outside Sama. Sama has no checkout, payment processing, payment-method storage, or billing-management role. See the [product boundary](docs/product.md#payments-stay-with-the-provider).

The accepted direction is a Go modular monolith, OIDC identity, encrypted provider credentials, PostgreSQL-backed operations, and independently built frontend/backend artifacts assembled into one release image. The core stack is fixed; updates within it are welcome, replacement debates are not.

Selected refinements: PostgreSQL plus bounded in-process caching without Redis; TanStack Query/React Router/local React state for frontend state ownership; UUIDv7 entity identifiers internally and in the API. See [decision records](docs/decisions/README.md) for rationale and status. These remain architecture only.

The [MIT license](LICENSE) permits commercial use, resale, modification, and proprietary derivatives, subject to retaining its notice. It includes standard warranty/liability disclaimers. Future releases should be evaluated in an isolated sandbox before production; this is operational guidance, not an added license condition.

## AI collaboration

[AGENTS.md](AGENTS.md) routes agents to the shared rules under [docs/agents/](docs/agents/README.md). Conversation, input, research, and command records stay local in the ignored `agents/` folder. This documentation workflow is independent of application linting; the standalone Python collaboration workflow is available under `scripts/`.

## Project documents

[Contributing](CONTRIBUTING.md) · [Security](SECURITY.md) · [Community conduct](CODE_OF_CONDUCT.md) · [MIT license](LICENSE)

Sama is not affiliated with the providers it plans to support. No hosting organization or GitHub remote has been selected.
