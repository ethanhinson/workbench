---
id: 2
slug: workbench-init-onboarding
title: workbench init — one-command onboarding
status: proposed
priority: high
type: feat
created: 2026-08-23
updated: 2026-08-23
depends_on: []
related: [3]
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

The review named installation/distribution as the highest-leverage improvement
right now — higher than new board features — because the architecture is already
differentiated and the main barrier is adoption friction. Current setup is
clone + go build + manual MCP registration + skill install.

## What changes

A `workbench init` subcommand that:
1. Detects installed coding harnesses (Claude Code, Codex, etc.).
2. Registers the MCP server at user scope.
3. Installs the appropriate skills.
4. Verifies connectivity (round-trips an MCP call).
5. Starts / configures the local viz endpoint.
6. Prints a short success summary.

## Out of scope

- Package-manager distribution (brew/npm) — tracked separately as #3.
- Windows harness detection (first cut targets macOS/Linux).

## Open questions

- Which harnesses to detect in the first cut, and how (config-file probing vs. known paths)?
- Idempotency: how does re-running `init` behave when an MCP registration already exists?
- Where does skill installation write, per harness?

## Reconcile log
