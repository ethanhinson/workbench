---
id: 8
slug: docket-trivial-build-ready
title: kanban-docket — map trivial:true to Build-Ready lane
status: proposed
priority: medium
type: fix
created: 2026-08-25
updated: 2026-08-25
depends_on: []
related: []
discovered_from: []
adrs: []
spec:
plan:
results:
trivial: true
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

A docket change with `trivial: true` is build-ready with no separate spec — its
manifest body IS the spec. But the `kanban-docket` SKILL.md mapping table had no
`trivial` rule, so trivial changes fell through to "proposed, no spec → needs_spec"
and rendered as un-spec'd on the board (e.g. change 0003 package-distribution
sitting in Needs Spec when it was actually build-ready).

## What changes

- Add a `trivial: true (proposed, no branch/pr) → view:backlog, lane:build_ready`
  row to the mapping table in `skills/kanban-docket/SKILL.md`, above the plain
  "proposed, no spec" row so it takes precedence.

## Out of scope

- The benchmark fixture copy under `benchmarks/adoption/scenarios/.../kanban-docket/`
  (frozen baseline).

## Open questions

<!-- none — one-line doc rule addition -->

## Reconcile log
