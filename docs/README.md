# Sama documentation

This index describes the product and its stable design. Development commands and contributor checks live in [Contributing](../CONTRIBUTING.md).

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

## Reading guide

For product decisions: product → providers → roadmap. For implementation: architecture → security → data → operations → API → frontend. For repository navigation: repository structure → architecture. For operational design: deployment → security → operations. New adapters must follow the checklist in [providers](providers.md#adapter-acceptance-checklist).

## Vocabulary

- **Workspace:** the authorization boundary for a person or team within Sama.
- **Connection:** one provider account/project/service credential scope inside a workspace. One brand can require several connections.
- **Provider resource:** an object managed externally. Sama stores an observation, not ownership of the infrastructure.
- **Capability:** a specific supported resource operation, qualified by provider, account permissions, and resource state.
- **Operation:** a durable user intent and its execution history. It is not complete merely because a provider accepted an HTTP request.
- **Reconciliation:** reading the provider’s state to establish what actually happened.
