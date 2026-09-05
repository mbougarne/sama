# Efficiency and resource rules

Efficiency means reaching a trustworthy result with the least unnecessary computation,
I/O, network use, tool output, and repeated work. It never justifies skipping a required
safety or correctness check.

- **E-001 — Start narrow and widen on evidence.** Inspect named files and relevant search
  results first. Expand to a module, repository, or external source only when the narrow
  evidence is insufficient. *(Origin: whole-repository scans when a bounded source set
  answered the question.)*

- **E-002 — Reuse fresh evidence.** Do not rerun an unchanged command, reread the same
  large file, or regenerate an unchanged artifact unless freshness or reproducibility is
  itself under test. Record reusable results and their scope. *(Origin: repeated
  verification without an intervening state change.)*

- **E-003 — Use a verification ladder.** Run the smallest relevant static or focused
  check while working, then one proportionate full gate at the end. Add adversarial,
  integration, or real-resource probes only where the claim requires them. *(Origin:
  expensive full-suite repetition and underpowered static-only runtime claims.)*

- **E-004 — Bound concurrency.** Parallelize only independent work that reduces elapsed
  time. Do not duplicate the same reading or verification across agents, and do not
  exceed the environment's declared concurrency limit. *(Origin: duplicated parallel
  investigations and resource contention.)*

- **E-005 — Bound every long-running operation.** Use relevant timeouts, output limits,
  pagination, and scoped targets. Never leave background processes, containers, locks,
  or temporary resources running after the task. *(Origin: hanging probes, oversized
  logs, and leaked local resources.)*

- **E-006 — Keep context small and authoritative.** Load rules, generated handoff state,
  and the few records relevant to the task; do not inject an entire historical archive
  when a generated index or bounded search can route the work. *(Origin: large rolling
  context files that were both stale and expensive.)*

- **E-007 — Avoid unnecessary dependencies and network calls.** Prefer repository tools
  and already-available dependencies. Batch independent lookups, cache safe reusable
  downloads, and request installation or network access only when it materially advances
  the task. *(Origin: avoidable setup work and repeated dependency downloads.)*

- **E-008 — Control data volume.** Stream large inputs, cap individual and total scan
  sizes, summarize noisy output, and fail clearly when a safe budget is exceeded. Never
  load an unbounded diff, log, binary, or directory tree into memory. *(Origin: script
  review of unbounded repository fingerprints and ledger scans.)*

- **E-009 — Match capability to risk.** When the environment permits a capability or
  model choice and the user has not selected one, use the least expensive option that can
  reliably satisfy the task. Do not trade away safety or correctness for cost on
  security-sensitive, destructive, or high-uncertainty work. *(Origin: cost-aware agent
  operation without quality dilution.)*

- **E-010 — Stop at the terminal condition.** Once the requested outcome is implemented
  and proportionately verified, report it. Do not add speculative improvements, extra
  artifacts, or another verification cycle without new evidence or a request. *(Origin:
  scope growth after a task was already complete.)*

- **E-011 — Choose a proportionate tracked profile.** Repository owners select the
  smallest collaboration profile that preserves the evidence they actually need. Use
  `full` when educational research and command accountability are requirements, `lean`
  when conversation continuity is sufficient, and `history-only` when a compact durable
  index is the intended boundary. Agents must follow the tracked choice and may not
  disable artifacts ad hoc to save work. *(Origin: public consumers have different token,
  storage, privacy, and audit budgets.)*
