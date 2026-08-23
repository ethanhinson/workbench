---
id: 5
slug: strengthen-sync-reliability
title: Strengthen sync — session-live toward reliable
status: proposed
priority: medium
type: refactor
created: 2026-08-23
updated: 2026-08-23
depends_on: []
related: []
discovered_from: []
adrs: []
spec:
plan:
results:
trivial: false
auto_groomable:
branch:
pr:
blocked_by:
reconciled: false
---

## Artifacts

<!-- docket:artifacts:start (generated — do not hand-edit) -->
<!-- docket:artifacts:end -->

## Why

The review's sharpest tradeoff: synchronization is behavioral (the harness must
follow the skill and update the board), which introduces model-behavior failure
modes on top of ordinary engineering ones. It is honestly "session-live," not
"strongly synchronized." The adoption benchmark showed the failure mode directly:
before broadening trigger language, agents often ignored the board.

## What changes

Investigate and, where warranted, implement ways to raise update reliability without
abandoning the skills-as-adapters design:
- stronger / broader skill trigger language (partially done per the benchmark),
- a lightweight "reconcile board" step the agent runs at defined checkpoints,
- optional deterministic fallback importers for the shipped methodologies,
- a session-end completeness check.

## Out of scope

- Filesystem watchers / daemons / cron importers — these would break the
  disposable-projection property, which must be preserved.

## Open questions

- Where is the right point on the flexibility ⇄ reliability curve?
- Can a deterministic fallback coexist with agent hydration without becoming a second source of truth?
- What is the measurable target (benchmark adoption %)?

## Reconcile log
