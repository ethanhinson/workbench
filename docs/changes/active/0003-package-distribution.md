---
id: 3
slug: package-distribution
title: Package distribution — brew / npm
status: proposed
priority: medium
type: chore
created: 2026-08-23
updated: 2026-08-23
depends_on: []
related: [2]
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

The distribution half of the review's install/onboarding recommendation: move from
"git clone + go build" to a package-manager install so the tool is trivially
obtainable. Pairs with the `workbench init` onboarding flow (#2).

## What changes

- Homebrew tap/formula for the static Go binary.
- npm global wrapper (or per-platform Go release binaries).
- Versioned GitHub releases publishing prebuilt binaries.

## Out of scope

- The `init` onboarding flow itself (#2).
- Auto-update mechanics.

## Open questions

<!-- mechanical once release tooling is chosen; brew vs npm priority is a small call -->

## Reconcile log
