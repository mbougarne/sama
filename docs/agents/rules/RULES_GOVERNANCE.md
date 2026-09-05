# Rules governance

Rules are distilled from evidence; they are not accumulated from preferences guessed by
an agent.

## Rule format

Every accepted rule has a permanent ID, one imperative statement, a short scope note,
and a concrete origin. IDs are never reused. Repository rules should use their own
namespace if `C-` or `K-` would collide.

## Candidate loop

A rule candidate is appropriate when an authorized maintainer explicitly says `rule:`, a
correction repeats, a correction requires multiple exchanges, a change is reverted for a
reusable reason, or empirical evidence reveals a general failure class.

At the next natural pause, propose:

> Rule candidate (ID): statement. Origin: incident. Add it?

Only an authorized maintainer's acceptance authorizes adding it. Rejected candidates are
recorded in the event so they are not repeatedly proposed. A `rule:` message receives
priority, but urgent safety containment may happen before writing the prose.

## Amendment and retirement

Accepted amendments keep the original origin and add an amendment date and reason.
Rules are not deleted; mark them `DEPRECATED YYYY-MM-DD` and identify the replacement.

## Precedence

System and user instructions outrank every repository document. Within repository
documents, the repository-specific rule wins over this shared standard. The agent must
report a conflict rather than silently choosing a convenient interpretation.

## Retrospective

When requested, or roughly every twenty material records, inspect recent corrections and
rule references. Propose only repeated, generalizable boundaries. Do not turn a one-off
implementation detail into a global rule.
