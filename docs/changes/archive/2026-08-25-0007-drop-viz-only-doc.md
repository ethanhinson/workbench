---
id: 7
slug: drop-viz-only-doc
title: Drop the stale --viz-only browse snippet from README
status: done
priority: low
type: docs
created: 2026-08-25
updated: 2026-08-25
depends_on: []
related: [1]
discovered_from: []
adrs: []
spec:
plan:
results:
trivial: true
auto_groomable:
branch: feat/0007-drop-viz-only-doc
pr: https://github.com/ethanhinson/workbench/pull/3
blocked_by:
reconciled: false
---

## Artifacts

<!-- docket:artifacts:start (generated — do not hand-edit) -->
<!-- docket:artifacts:end -->

## Why

`--viz-only` is deprecated — browsing is now served by the MCP process on `:7777`
itself (a single process serves both MCP-over-stdio and the viz HTTP UI). The README
still documents `--viz-only` as the way to "browse boards without an agent"
(README.md:141-144), which is now misleading. Remove the stale snippet.

## What changes

- Remove the `# browse boards in a database without an agent` code block from README.md.

## Out of scope

- Removing the `--viz-only` flag from the binary itself (separate, larger change).

## Open questions

<!-- none — mechanical doc deletion -->

## Reconcile log

### 2026-08-25

Implemented as a single doc deletion on `feat/0007-drop-viz-only-doc`; PR #3
squash-merged to `main` (`56289c9`). The stale `--viz-only` browse snippet is gone
from README. Archived to done.
