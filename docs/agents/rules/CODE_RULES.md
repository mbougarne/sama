# Cross-repository code rules

Apply a rule only where its technology and boundary are relevant. Repository-specific
rules may narrow or replace these defaults.

- **K-001 — Implement one smallest complete unit.** State non-goals and keep unrelated
  cleanup out of the change. *(Origin: staged application and infrastructure tickets.)*

- **K-002 — Fail closed and explain why.** A gate, harness, or validator must reject its
  known-bad input class with a useful message; silent fallback is a defect. *(Origin:
  test-database fallback, plaintext-secret validation, and unreachable SOPS guards.)*

- **K-003 — Probe runtime claims.** Grants, recovery, cleanup, networking, rate limits,
  and lifecycle behavior require execution when feasible. Re-probe a fix expecting the
  opposite result. *(Origin: empirical infrastructure review practice.)*

- **K-004 — Tie evidence to a snapshot.** Record the commit and repository-state digest
  for the exact staged, working-tree, or commit scope that was tested or reviewed.
  *(Origin: revert/reapply evidence not tied to the final repository snapshot.)*

- **K-005 — Prefer portable repository behavior.** Shared code, CI, and documentation
  must not require personal profile names, shell helpers, absolute user paths, or
  untracked local binaries. *(Origin: local Colima and tool-path corrections.)*

- **K-006 — Anchor destructive paths.** Removal or rewrite targets must resolve from a
  validated repository/script root and have a sentinel test proving outside invocation
  cannot escape that root. *(Origin: contract-generation path escape hazard.)*

- **K-007 — Pin build and generation tools.** Tool versions that affect generated output
  or acceptance must be reproducible across local and CI environments. *(Origin: Buf,
  OpenTofu, Go toolchain, and package-lock findings.)*

- **K-008 — Keep service and trust boundaries explicit.** A service owns its data;
  browsers do not call internal administrative services or hold service credentials;
  trusted BFF/API boundaries remain authoritative. *(Origin: billing and administrative
  service architecture.)*

- **K-009 — Propagate asynchronous failures.** In promise-based request handlers, rejected
  work reaches the framework error boundary; fire-and-forget promises are not used where
  failure matters. *(Origin: asynchronous request middleware.)*

- **K-010 — Keep test tooling out of production artifacts.** Production images and build
  inputs exclude test-only configuration and dependencies unless runtime use is proven.
  *(Origin: test-configuration production-image reversals.)*

- **K-011 — Test behavior, not echoes.** Assertions must discriminate correct behavior
  from a plausible mutation; tests that merely repeat an input provide no useful proof.
  *(Origin: review mutation-score finding.)*

- **K-012 — Match the owning abstraction and local style.** Centralize only genuinely
  generic logic; keep domain behavior with its owner; make new code read like its
  surrounding file. *(Origin: PostgreSQL helper and shared-layer boundary corrections.)*
