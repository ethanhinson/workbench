---
id: 4
slug: reposition-observability
title: Reposition docs — observability, not another task tracker
status: proposed
priority: high
type: docs
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

The review's central strategic point: positioning Workbench as "a live kanban board
for your coding agent" drops it into a crowded category and hides the actual
architectural idea — a disposable live projection over an independently-owned
methodology. The defensible framing is "observability for agentic development, not
another task tracker; your methodology is the source of truth, Workbench is the view."

## What changes

- Lead the README with the projection/observability framing.
- Make the "state is disposable / derived" property prominent.
- Add the architecture diagram (methodology → agent → generic MCP → SQLite → UI).
- Add the competitor-differentiation table (who owns methodology / queue / execution / viz).

## Out of scope

- Renaming the project or MCP tools.
- Building any of the "layouts beyond kanban" future direction (#6).

## Open questions

- How much of the competitive analysis belongs in the README vs. a separate positioning doc?
- Keep or drop the "kanban" word in the tagline?

## Reconcile log
