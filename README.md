# Sama · سماء

**Your cloud, clearly.** Sama is a proposed open source, self-hosted web application for managing cloud products through provider APIs. سماء means “sky” in Arabic.

## Current phase: architecture review

This repository contains documentation only. The architecture is a draft to read, discuss, and refine before implementation starts. There is no application, test suite, dependency installation, or deployment setup to run.

The requested foundation is a Go backend and a React frontend built with Webpack, in the same repository with separate **`backend/`** and **`frontend/`** directories. These directories will be created when coding is explicitly requested. Global project documents stay at the root.

## Start reading

1. [Product scope](docs/product.md): what Sama should do and how it should feel to use.
2. [Architecture overview](docs/architecture/README.md): components, boundaries, and proposed technology choices.
3. [Repository structure](docs/architecture/repository.md): clear backend/frontend ownership and the future folder layout.
4. [Provider research](docs/providers.md): API findings and integration differences.
5. [Roadmap](docs/roadmap.md): architecture review first, then proposed implementation phases.

The [documentation index](docs/README.md) links to the detailed data, API, security, operation lifecycle, frontend, deployment, and decision documents.

## Proposed design

A Go application with separate internal modules, provider-specific adapters, a React + TypeScript interface, and PostgreSQL for durable state. Common workflows should be simple while preserving each provider’s native capabilities. Credentials remain on the backend; cloud changes have explicit permissions, review, and traceable outcomes.

**Sama manages products, never payments.** Users connect their own provider accounts and manage supported resources through APIs, including creating resources. Each provider bills the user directly and handles payment entirely outside Sama. Sama has no checkout, payment processing, payment-method storage, or billing-management role. See the [product boundary](docs/product.md#payments-stay-with-the-provider).

PostgreSQL, OIDC, the job design, deployment packaging, and provider delivery order are proposals for review. The current documents do not imply approval to implement them.

## Project documents

[Contributing](CONTRIBUTING.md) · [Security](SECURITY.md) · [Community conduct](CODE_OF_CONDUCT.md) · [Apache-2.0 license](LICENSE)

Sama is not affiliated with the providers it plans to support. No hosting organization or GitHub remote has been selected.
