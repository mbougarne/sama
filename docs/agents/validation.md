# Collaboration tooling and validation

The full [rules](README.md) govern behavior. Python tooling validates observable record properties independently of backend/frontend linting. Run it with Python 3.10+ and Git on macOS/Linux; only the Python standard library is used. Windows is not supported by the inherited POSIX lock implementation. These Python collaboration commands require no package installation. The separate opt-in Git hook and CI workflow are described in [Contributing](../../CONTRIBUTING.md#pre-commit).

## Commands

From the repository root:

```sh
python3 scripts/collab.py build .
python3 scripts/collab.py new . short-topic --agent Codex --role analyst --input-file /path/to/sanitized-input.txt
```

`build` initializes an empty store and preserves existing history; it does not rebuild or reseal final records. `new` prints an event UUID, conversation UUID, and four paths. Use an observed agent name and actual role; `analyst`, `implementer`, `reviewer`, and `coordinator` are supported. The input file must contain the complete sanitized user message, not host instructions. Do not put secrets in command arguments or in the input file.

For subsequent input in the same chat, add `--conversation <printed-conversation-uuid>`. For a correction, also add `--supersedes <earlier-final-event-uuid>`. Complete all printed Markdown drafts; keep the input text intact except for necessary redactions.

```sh
python3 scripts/collab.py finalize . <printed-event-uuid> --evidence-scope working-tree
python3 scripts/collab.py lint .
python3 -B -m unittest discover -s scripts/tests -v
```

Select `working-tree` for current changes, `staged` for the exact Git index under review, or `docs-only` to label document-focused evidence. The inherited `docs-only` fingerprint covers the working tree; it does not filter out non-document changes. A fingerprint identifies state, not whether reported checks happened. Handoff is read from the latest relevant final conversation file through the index.

`lint` does not rewrite content or hashes. It uses the operational lock file, so that filename can be created even by lint on an existing store. With no `agents/` directory, lint succeeds without creating it: a fresh public clone has no private history to require.

## What is checked

- JSON shape, duplicate keys, UUID identity, consecutive per-conversation sequence, correction targets, and event status.
- Four exact matching paths and frontmatter; missing/orphan files; final byte digests and new-event metadata digests.
- New-event conversation evidence/handoff, full-input presence, AI research sections and labels, unique commands with What/Why, and unfinished placeholders.
- Known credential patterns throughout the local store, including unindexed files; findings identify locations and categories without printing matched credentials.
- Path containment, symlinks, hard links and nonregular files; per-file, total-scan, file-count and Git-fingerprint budgets.

`new`, `finalize`, and existing-store lint share a bounded POSIX lock. Four-file creation rolls back its newly created files on an ordinary write exception; history replacement is atomic. A killed process between file creation and index replacement may leave orphans; lint reports these instead of silently discarding them. The shared engine fsyncs file content but does not promise power-loss durability of directory entries.

## Recovery and limits

Do not edit finalized records or use `build` to bypass a mismatch. Restore exact original bytes only from a verified copy matching the stored digest, and record the recovery in a new exchange. Otherwise report the mismatch for a scoped recovery decision. Do not delete unexplained orphans or change old checksums to make lint pass. Interrupted drafts may be completed after inspecting actual work; placeholders and unverified claims must not be finalized.

The first `sama-local-v1` event remains unchanged and receives structural, privacy and file-digest validation. New events use `sama-local-v2`, including mandatory AI learning sections and history metadata seals. The old event is not retrospectively judged against new content requirements.

Checksums stored with content are not independent tamper-proof evidence: coordinated rewriting of index and files, or deletion of the newest event, needs an external anchor to detect. Credential patterns are heuristic, not exhaustive secret detection. Cooperative locks do not stop editors or hostile processes from changing files concurrently; one writer owns each draft and formatters must exclude `agents/`. No automatic backup/export or privacy guarantee is implied.

## Ownership

- `docs/agents/`: behavioral policy, record contract, research intent.
- `scripts/collab.py`: Sama lifecycle and validation entry point.
- `scripts/agent_collaboration/`: vendored engine, provenance, templates.
- `scripts/tests/`: focused collaboration-tool checks.
- `agents/`: ignored local records and operational lock only.

Backend/frontend formatters, linters, builds and tests stay in their own directories. Public CI never requires private records. The explicitly requested hook is installed through scripts/install_hooks.py; it checks local records and the staged application tree without replacing existing unrelated hooks.
