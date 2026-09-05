# Collaboration rules

These are shared defaults. A repository's own rules win for that repository; conflicts
must be reported so its maintainers can reconcile them. Each rule has a stable ID and an
origin.

## Evidence and communication

- **C-001 — Qualify every success claim.** State what was verified, how it was verified,
  the exact evidence scope, and what was not run. Local, staged, CI, real-resource, and
  production acceptance are distinct evidence levels. *(Origin: qualified-green and
  unavailable-lifecycle findings in legacy project ledgers.)*

- **C-002 — Answer the question first.** Lead with the requested classification,
  decision, verdict, or outcome; add only the evidence needed to support it. *(Origin:
  repeated classification and status exchanges in legacy project ledgers.)*

- **C-003 — Push back with reproducible evidence.** Fix valid findings and refute invalid
  ones with a concrete reason or probe. Authority alone is not evidence. *(Origin:
  generated-code and mutation-score review triage.)*

- **C-004 — Report blockers instead of hiding them.** Classify a blocker as an
  environment prerequisite, code defect, external ceremony, missing authority, or
  structurally impossible criterion. Never silently weaken a gate. *(Origin: clean-host
  and external-provider acceptance boundaries.)*

- **C-005 — Use the authoritative status source.** A merge, record, or handoff does not
  override the repository backlog, accepted ticket state, code, or current environment.
  *(Origin: stale handoffs across legacy project ledgers.)*

## Scope and authority

- **C-006 — Make the smallest complete change.** Stay inside the named task and avoid
  speculative refactors. Read-only investigation and ordinary verification are allowed
  when relevant; material scope expansion requires direction. *(Origin: staged ticket
  and foundation work across both repositories.)*

- **C-007 — Respect the authorization envelope.** Read-only inspection, local tests,
  synthetic identities, and disposable local resources are normally allowed. External
  writes, real credentials, cloud/provider changes, billable resources, and destructive
  operations require explicit authority unless the repository states otherwise.
  *(Origin: infrastructure and security work with external effects.)*

- **C-008 — Preserve reviewer independence.** A review does not edit implementation
  code. It may perform safe read-only verification within scope; findings and evidence
  go into a durable review artifact with an explicit verdict. *(Origin: staged review
  practice across multiple repositories.)*

## Continuity and conduct

- **C-009 — One input, one event, configured evidence.** Every user input gets exactly
  one event in the repository's configured profile. The `full` profile creates one
  conversation record, one research trace, and one command inventory with the same
  `event_id`; history-only creates one canonical history entry. Never merge separate
  inputs or split one input across multiple events. Keep low-complexity events brief
  instead of skipping them. *(Origin: missing research and loss of prompt-to-action
  causality when records were optional or consolidated.)*

- **C-010 — Corrections append; history does not rewrite.** A correction creates a new
  event with `supersedes_event_id`; finalized records are never edited. *(Origin:
  append-only continuity requirements and semantic sequence collisions.)*

- **C-011 — Preserve the requester's voice safely.** Include one sanitized verbatim
  sentence in each event record. Remove secrets and unnecessary personal data, not
  meaning or tone. *(Origin: loss of intent in summary-only continuity records.)*

- **C-012 — No secrets, private reasoning, or fake internals.** Never record credentials,
  sensitive personal data, hidden reasoning, or invented values presented as model
  telemetry. Educational values must be labeled observed or illustrative. *(Origin:
  security scanning and boilerplate-research defects in both legacy systems.)*

- **C-013 — Make handoffs derived and current.** Handoff state lives in immutable final
  records; `NEXT_CONVERSATION.generated.md` is rebuilt from them. Never hand-maintain a
  second version of current state. *(Origin: stale `current.md`, `NEXT_CONVERSATION.md`,
  and cumulative `latest-interaction.txt` projections.)*

- **C-014 — Be direct under correction.** State the fault, the changed behavior, and the
  remaining evidence. Do not defend a disproven claim or erase the correction from the
  record. *(Origin: multi-exchange correction loops in legacy project work.)*

- **C-015 — Batch work and verify proportionally.** Run focused checks while working,
  the appropriate full gate once at the end, and adversarial or known-bad probes for
  gates and runtime claims. Do not repeatedly rerun unchanged proof. *(Origin: empirical
  validation and operating-pace lessons across both repositories.)*

- **C-016 — Enabled research is mandatory and educational.** When `research_traces` is
  enabled, every new event has exactly one research trace. Teach one concrete concept
  with worked mechanics tied to the exchange,
  or state exactly `No concept fit this exchange.` and briefly explain why. Do not use a
  generic attention, token, vector, or probability template merely to fill the record.
  Prefer a concept not taught in the previous five traces. Observed and illustrative
  values must be labeled, and generated explanations must never be presented as private
  reasoning or model telemetry. *(Origin: educational traces disappeared when optional
  and became boilerplate when their semantic floor was underspecified.)*

- **C-017 — Enabled commands are a deduplicated per-input inventory.** When
  `command_inventories` is enabled, every new event has exactly one command file. Record
  each distinct command once, even if it ran repeatedly, with a
  brief description of what it does and why it was used for this input. Normalize
  insignificant whitespace when deciding whether commands are duplicates; keep
  materially different arguments as separate entries. If no command ran, state exactly
  `No commands were run.` Never record command output, secrets, credentials, private
  URLs, or unnecessary machine-specific paths. *(Origin: command activity was either
  absent from continuity or repeated as a noisy execution log.)*
