# Local collaboration records

Sama uses a fixed four-file record policy for new inputs from adoption onward. The history container retains `sama-local-v1`; new event entries and Markdown use `sama-local-v2`. The original v1 event remains immutable. This is Sama’s adapter format, not the upstream CLI’s bundle schema. These are local continuity records, not Sama product data or public project documentation.

## Paths and naming

```text
agents/                         Ignored in full; local to this checkout
├── history.json                Index and final-file checksums
├── .history.lock               Operational lock; not a content artifact
├── conversations/
│   └── <stem>.md               Outcome, evidence, decisions, handoff
├── inputs/
│   └── <stem>.md               One complete sanitized user input
├── researches/
│   └── <stem>.md               Sources, findings, useful explanation
└── commands/
    └── <stem>.md               Unique commands and their purpose
```

Use the same **full filename** in all four folders:

`<UTC YYYY-MM-DD-HH-MM-SS>_<event UUIDv4>_<sequence padded to four digits>.md`

For example, an illustrative event uses `2026-09-05-10-30-00_5c95b486-8408-46e6-aebd-9b3427a32691_0001.md` in each folder. Sequence numbering continues past four digits if needed. `history.json` is the only differently named content file. Do not introduce `latest.md`, topic aliases, UUID-only companion names, or extra handoff/view files.

One conversation may contain many user inputs. Each input gets a new event UUID, its own matching four files, and the next sequence within that conversation. A follow-up never overwrites the previous input. Maintain one locally allocated conversation UUIDv4 for the actual chat; reuse it when continuing that chat. Record `conversation_id_source: local-allocation`; do not present this UUID as the host's task ID or fabricate host/model identities. Distinct chats use distinct conversation UUIDs. If continuity cannot be established, start a new local conversation and describe the gap.

## File contents

Each Markdown file starts with simple YAML frontmatter containing `format: sama-local-v2`, `event_id`, `conversation_id`, `sequence`, `recorded_at` (UTC RFC3339), and `kind` (`conversation`, `input`, `research`, or `commands`). Quote UUIDs and timestamps. All four files agree on shared metadata.

| File | Required content |
| --- | --- |
| Conversation | Request summary and input reference; interpretation and assumptions; actions and changed paths; verification with explicit **Verified** and **Not run**; decisions and corrections; uncertainty; handoff containing Done, Next step, Blockers, Reading order. Identify actual agent/role and evidence scope. |
| Input | Complete user-supplied message in its original language and ordering, with only necessary sanitization. Label redactions; identify unavailable attachments without claiming they were captured. Do not include system/developer messages or tool-injected environment context. |
| Research | AI learning content according to the [research contract](research.md): observable context, one AI concept, a worked example, task connection, limits, sources, and learning experiment. Include `concept` and `value_kind` metadata; body at most 100 lines. No generic project-research replacement. |
| Commands | Each distinct executed shell command or tool operation once, with **What** and **Why**. Deduplicate repeat runs while retaining materially different arguments. Put results in conversation verification, not here. If none ran, state `No commands were run.` |

For large or multiline commands, use a fenced block. Replace sensitive or unnecessary machine-specific arguments with explicit redaction labels; do not call a sanitized command a verbatim transcript. Never save credentials, private URLs, sensitive personal data, hidden reasoning, or full tool transcripts. Ordinary technical prompts stay complete; record any redaction reason without retaining the removed value.

## History and lifecycle

Use the [CLI](validation.md) to initialize `history.json` to an object with `format: "sama-local-v1"` and `events: []`. Each event entry contains:

| Field | Meaning |
| --- | --- |
| `format`, `entry_sha256` | New events use `sama-local-v2`; final entries seal their metadata with a digest excluding this digest field |
| `event_id`, `conversation_id`, `conversation_id_source`, `sequence`, `recorded_at` | Identity and order; UUIDs allocated as described above |
| `topic`, `agent`, `role` | Short topic, observed agent identity, role in this input |
| `status`, `finalized_at` | `draft` with null finalization time, then `final` with UTC time |
| `supersedes_event_id` | Earlier corrected event UUID, or null |
| `files` | Object with `conversation`, `input`, `research`, `commands` paths relative to `agents/` |
| `sha256` | Empty object while draft; at finalization the same four keys contain each file's lowercase SHA-256 digest |
| `evidence` | `scope`, `repo_head`, `repo_branch`, `repo_state_sha256`, and concise `verification`; use `unborn` for a repository without a commit |

1. Use `new` from [the validation workflow](validation.md) to capture the complete sanitized input and allocate paired drafts. The CLI verifies ignored/untracked paths, rejects symlinks, and serializes creation with a lock. One writer owns each event’s draft content.
2. Complete all four drafts and meaningful verification. Do not overwrite another event. Preserve a follow-up’s own event even when it steers an ongoing task.
3. Use `finalize` with the event UUID and evidence scope. It validates content, records the repository fingerprint, hashes exact file bytes, seals metadata, and atomically finalizes the history entry. Do not hand-edit the index.
4. Run `lint` before ending the exchange. It reports unfinished drafts, mismatches, unsafe paths, known credential patterns and unexplained files. Do not bypass a failure by weakening the checks.
5. Final records and entries are immutable. A correction appends a new event using `--supersedes`; a verified original may be restored exactly after an accidental formatting change, with recovery recorded separately. See [recovery limits](validation.md#recovery-and-limits). Never silently recalculate an old checksum.

Read handoff content from the latest relevant finalized conversation record. History indexes the files; it does not replace their evidence. Do not backfill earlier messages from a summary or claim historical coverage. If requested to import an exact earlier transcript, identify it as imported and preserve its provenance.

## Local-only boundary

Root `.gitignore` excludes `/agents/`. Verify this before creating records; never force-add this folder. Share durable, sanitized architectural findings through ordinary `docs/` changes when relevant. Contributors receive the shared rules, not another person's transcripts.

Git ignore is not encryption or backup. The installed checks provide cooperative locking, content validation and credential-pattern detection; they do not establish tamper-proof storage, exhaustive secret detection, or automatic recovery. See [tooling limits](validation.md#recovery-and-limits). Export/backup needs explicit scope and destination. Exclude `agents/` from formatters: even adding blank lines changes a finalized file’s digest.
