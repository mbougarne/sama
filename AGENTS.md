# Working on Sama

Sama (سماء, sky) is in architecture stabilization. Application implementation has not started.

Before proposing, editing, reviewing, or recording work, read these tracked documents in order:

1. [Collaboration contract](docs/agents/README.md) — read all four linked rule files in order, including governance, then the explicit Sama adaptations.
2. [Sama project rules](docs/agents/project.md) — current scope, fixed stack, product boundaries, accepted direction.
3. [Local record format](docs/agents/records.md) — one input per event, exact paths and naming, corrections and handoffs.
4. [AI research contract](docs/agents/research.md) — educational AI mechanics tied to the exchange, with observed facts separated from illustrative examples.
5. [Validation boundaries](docs/agents/validation.md) — present checks, limitations, and separation from application linting.

Then read [README.md](README.md), [the documentation index](docs/README.md), and the architecture relevant to the request. These paths are relative to the repository root. Do not substitute an external standard, a skill, or a historical record for this contract.

Record new user inputs under ignored `agents/`, using matching filenames in `conversations/`, `inputs/`, `researches/`, and `commands/`, indexed by `history.json`. Keep records local; never force-add them. Start from the latest relevant finalized handoff when present, then verify against the current checkout.

Shared rules live in `docs/agents/`; runtime records live in `agents/`. Use `python3 scripts/collab.py new` for each input, complete the four drafts, then `finalize` and `lint` before finishing. The Python collaboration tooling and its focused tests are explicitly authorized; its behavior is documented in the validation workflow. This preparation does not authorize backend/frontend coding, application tests, dependencies, builds, or deployment.
