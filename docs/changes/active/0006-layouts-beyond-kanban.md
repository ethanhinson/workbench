---
id: 6
slug: layouts-beyond-kanban
title: Layouts beyond kanban — generic visual substrate
status: proposed
priority: low
type: feat
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

The review's strategic assessment: the strongest long-term direction is not the best
kanban board but the generic visual substrate for arbitrary agentic workflows. The
layout-as-data model already points there — the information architecture is
agent-authored, so non-kanban layouts are a rendering/primitive question, not a
methodology-backend question.

## What changes

Gap analysis of the current layout primitives (nav + views: list/lanes/board/doc)
against target layout kinds, then prioritized stubs for the 1–2 highest-leverage ones:
- planning dashboards, dependency graphs, review queues, spec trees,
  execution timelines, decision logs, agent activity streams,
  release-readiness views, research workspaces, multi-agent coordination.

## Out of scope

- Implementing all of the above — this spike produces analysis + prioritized follow-up stubs.

## Open questions

- Which target layouts does the current view model already nearly express?
- What new primitive (graph edges? time axis?) unlocks the most targets per unit of work?

## Reconcile log
