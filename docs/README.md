# Sama documentation

Status: **architecture baseline stabilized on 2026-09-05; application architecture only; collaboration tooling available**. The owner accepted the existing ADR decisions and fixed the core stack. Licensing, caching, frontend state, and identifier refinements are recorded with their selection status in [decision records](decisions/README.md). None of this authorizes implementation or claims working software.

| Read | Purpose |
| -------------------------------------------------------- | --------------------------------------------------------------------- |
| [AI collaboration](agents/README.md) | Shared agent rules, local records, and validation boundaries |
| [Product](product.md) | Audience, product principles, release scope, user journeys |
| [Architecture overview](architecture/README.md) | System boundaries, deployment topology, stack and tradeoffs |
| [Provider research](providers.md) | Source-backed API findings, capability differences, delivery order |
| [Data model](architecture/data.md) | Entities, tenancy, constraints, retention, transaction boundaries |
| [API and adapters](architecture/api.md) | HTTP contract, capability model, provider interfaces, errors |
| [Security](architecture/security.md) | Identity, authorization, encryption, threat model, safe changes |
| [Operation lifecycle](architecture/operations.md) | Durable jobs, retries, ambiguity, reconciliation and failure handling |
| [Frontend](architecture/frontend.md) | Interaction design, feature boundaries, data fetching, accessibility |
| [Deployment and reliability](architecture/deployment.md) | Deployment, backups, upgrades, observability and performance |
| [Architecture decisions](decisions/README.md) | Accepted direction, selected refinements, rationale, and future evidence |
| [Roadmap](roadmap.md) | Ordered implementation work and acceptance gates |
| [Repository structure](architecture/repository.md) | Selected backend/frontend folders, ownership, and build boundaries |

## This review's conclusions

| Topic | Direction |
| --- | --- |
| Community conduct | Technical scope, specific behavioral feedback, cultural awareness, fixed stack; participation rules do not restrict licensed use |
| Durable work and cache | PostgreSQL jobs and state; bounded in-process caches initially; no Redis dependency |
| License | Standard MIT; commercial/proprietary use and resale permitted with notice retention; sandbox guidance outside license terms |
| Frontend state | TanStack Query, React Router, local React state/reducers, narrow context; no duplicate global server-data store |
| Identifiers | UUIDv7 entity keys internally and externally; `id` is a UUID field; no default numeric surrogate |
| Phase | Continue architecture stabilization until implementation is explicitly requested |

## Reading guide

For product decisions: product → providers → roadmap. For implementation: architecture → security → data → operations → API → frontend. For repository navigation: repository structure → architecture. For operational design: deployment → security → operations. New adapters must follow the checklist in [providers](providers.md#adapter-acceptance-checklist).

## Vocabulary

- **Workspace:** the authorization boundary for a person or team within Sama.
- **Connection:** one provider account/project/service credential scope inside a workspace. One brand can require several connections.
- **Provider resource:** an object managed externally. Sama stores an observation, not ownership of the infrastructure.
- **Capability:** a specific supported resource operation, qualified by provider, account permissions, and resource state.
- **Operation:** a durable user intent and its execution history. It is not complete merely because a provider accepted an HTTP request.
- **Reconciliation:** reading the provider’s state to establish what actually happened.
