# Contributing to Sama

Read the [product scope](docs/product.md), [architecture](docs/architecture/README.md), and [repository structure](docs/architecture/repository.md). Explain the problem a change solves and its consequences for simplicity, security, and maintainability. Follow the accepted decisions; historical alternatives explain rationale rather than competing implementation options.

The Go, React + TypeScript, Webpack, and PostgreSQL stack is fixed. Follow the [conduct policy](CODE_OF_CONDUCT.md) and discuss specific technical issues rather than personal qualities. Cite official provider documentation for API claims, including the research date and unresolved questions.

Go source, dependencies and tooling belong under `backend/`; React/Webpack source, dependencies and tooling belong under `frontend/`. Global project documents stay at the root. Keep changes scoped to the agreed task.

Architecture documents describe stable boundaries, decisions and requirements. Change them only for an explicit architecture revisit or an agreed design amendment, not to record each message, implementation step or tool version. Keep command and setup instructions in these contributor guidelines and the owning project's README. Record task progress and handoffs in local collaboration records; check links when moving documentation.

AI-assisted work follows [AGENTS.md](AGENTS.md) and the [collaboration contract](docs/agents/README.md). Shared rules are versioned under `docs/agents/`; generated records under `agents/` must not be committed or attached to contributions. Promote relevant sanitized findings into the appropriate project documentation when they change its substance.

All contributions are under MIT. Contributors retain ownership of their work and must have the right to contribute it. No CLA or automated sign-off gate is configured. Commercial use, resale, private modifications, and derivative products are permitted under the [license](LICENSE), including notice retention. Contributor conduct governs shared project spaces; it does not add license restrictions.

## Development setup

- Go toolchain pinned in `backend/go.mod`.
- Node 24.20.0 LTS, pinned in `frontend/.nvmrc` and enforced by its package manifest.
- pnpm version declared by `frontend/package.json` (`packageManager`); use the same version locally and in CI.
- Python 3.10+ and Git for collaboration and hook helpers; CI uses Python 3.13.
- macOS/Linux are the supported local check environments. Race-enabled Go checks require a C compiler.

From the repository root:

```sh
cd frontend
nvm install
nvm use
pnpm install --frozen-lockfile --ignore-scripts
cd ..
python3 scripts/install_hooks.py
```

Use an equivalent Node version manager if preferred. The hook verifies the exact Node version in the staged `frontend/.nvmrc`. When the shell has another Node active, it automatically selects an already-installed matching nvm toolchain from `$NVM_DIR` or the standard `~/.nvm` location, so committing from a parent directory is safe. It does not install Node; equivalent manager users should activate the pinned version before committing. The hook also verifies the exact pnpm `packageManager` version. Go follows `backend/go.mod` and may download the required toolchain when `GOTOOLCHAIN=auto`; install it in advance for offline checks. A missing/wrong toolchain is a failing prerequisite, not a skipped check. Fresh clones must opt into the tracked hook using the installer because Git does not install repository hooks automatically.

## Local work

| Task | Command from repository root |
| --- | --- |
| Backend checks | `sh backend/scripts/check.sh` |
| Backend formatting | `sh backend/scripts/check.sh format` |
| Backoffice checks | `pnpm --dir frontend run check` |
| Backoffice formatting | `pnpm --dir frontend run format` |
| Collaboration/hook tests | `python3 -B -m unittest discover -s scripts/tests -v` |
| Local record checks | `python3 scripts/collab.py lint .` |

Start development servers separately with `cd backend && go run ./cmd/sama` and `cd frontend && pnpm run dev`. These local entry points require no database or provider credentials. Frontend checks include Jest component tests alongside formatting, lint, types and the production bundle. See [backend usage](backend/README.md) and [backoffice usage](frontend/README.md) for details.

## Pre-commit

The tracked `.githooks/pre-commit` invokes `scripts/pre_commit.py`. It checks finalized local collaboration records, exports the **Git index** into a temporary directory, then runs backend checks, installs the staged frontend lockfile from the local pnpm store, runs backoffice checks and tests the shared tools. Temporary builds and dependencies are cleaned up afterward.

The index and working files are never autoformatted or restaged. Bad formatting blocks the commit with instructions to format, review, and stage the intended changes. Partial staging is respected: a correct unstaged version cannot hide broken staged code, and an unfinished working edit does not replace the staged candidate being checked.

The hook uses `pnpm install --frozen-lockfile --offline --ignore-scripts --prod=false` for the exported snapshot. It resolves the frontend’s local pnpm store before creating dependencies in the temporary directory, so checks can reuse that store across volumes. Run `pnpm install --frozen-lockfile --ignore-scripts` in `frontend/` first to populate the cache for the lockfile being committed. The hook can take longer than lint alone because it validates both projects from the candidate commit. It rejects staged local records/build outputs, unresolved merges, symlinks and submodules; these are not part of the initial repository layout.

The temporary install is required because the staged snapshot deliberately cannot reuse working-tree `node_modules`; otherwise unstaged dependency state could make a broken commit look valid. The install is offline and uses `--ignore-scripts`. No development server is started: `pnpm run check` runs formatting validation, lint, type checks, component tests and a production bundle.

The installer refuses to replace a configured hooks directory or existing executable default hooks. Integrate those deliberately if present. Existing source index/working-tree changes are not altered by installation. Finalize AI records before committing; exclude ignored `agents/` from editor formatters. Root `.prettierignore` also excludes it.

## GitHub Actions

`.github/workflows/quality.yml` runs independent backend, backoffice, and collaboration-tool jobs on pushes and pull requests. The backend uses the same check script; the backoffice uses the same pnpm check task. No private history, secrets, cloud accounts, database services or deployment permissions are needed. A clean clone without `agents/` passes the local-record check.

The workflow has read-only repository permissions, time limits, cancellation of superseded runs, release-tagged actions and no persisted checkout credentials. Configure branch protection and required checks on the hosting repository; local hooks alone cannot enforce remote policy. A local run validates the check commands, while execution in GitHub must be verified separately.
