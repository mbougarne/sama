# Upstream provenance

Agent Collaboration Standard 1.3.0, scripts/collab.py, supplied by the project owner and inspected 2026-09-05. Source SHA-256: `47422738b400587dbf68f80577db032cae8b025c4756ab8bf406812b1a6e6537`. No upstream Git revision was available.

`upstream.py` preserves the original engine, with two integration changes: repository fingerprints exclude Sama’s `agents/` path instead of `.agents/`, and direct CLI execution is disabled. `scripts/collab.py` uses its parsing, content checks, privacy patterns, bounded Git fingerprints, locking, exclusive writes, and atomic-write helpers. Its original layout/profile commands are not invoked. Sama’s adapter owns naming, the four-file store, input capture, AI research validation, and history schema.

Behavioral rules live under `docs/agents/rules/`, copied in full. Sama-specific overrides are explicit in `docs/agents/README.md`. Neither source directory nor Zigide is a runtime dependency. Review changes to this vendored engine before updating it.
