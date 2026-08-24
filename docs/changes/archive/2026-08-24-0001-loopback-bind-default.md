---
id: 1
slug: loopback-bind-default
title: Bind viz HTTP to 127.0.0.1 by default
status: done
priority: high
type: fix
created: 2026-08-23
updated: 2026-08-24
depends_on: []
related: []
discovered_from: []
adrs: []
spec:
plan: docs/superpowers/plans/0001-loopback-bind-default-plan.md
results:
trivial: true
auto_groomable:
branch: feat/loopback-bind-default
claimed_at: 
pr: https://github.com/ethanhinson/workbench/pull/1
blocked_by:
reconciled: true
---

## Artifacts

<!-- docket:artifacts:start (generated — do not hand-edit) -->
| Artifact | Link |
|---|---|
| Plan | [0001-loopback-bind-default-plan.md](https://github.com/ethanhinson/workbench/blob/main/docs/superpowers/plans/0001-loopback-bind-default-plan.md) |
| PR | [#1](https://github.com/ethanhinson/workbench/pull/1) |
<!-- docket:artifacts:end -->

## Why

A competitive-analysis review flagged that the documented `--http :7777` binds on
all interfaces in Go, while the tool is described and intended as a local-only
developer surface. Actual network behavior should match that intent: a bare port
should listen on loopback only, with explicit opt-in for LAN/remote.

## What changes

- When `--http` is given a port-only value (`:7777`), default the bind host to `127.0.0.1`.
- Preserve explicit opt-in: `--http 0.0.0.0:7777` (or a named host) still binds externally.
- Apply the same normalization to the `KANBAN_HTTP` env var (it feeds the same flag)
  and to `--viz-only`'s built-in `:7777` fallback, which today binds all interfaces
  even when `--http` is unset.
- Update README examples and any UI "localhost" language to match actual behavior.

## Out of scope

- TLS / auth on the viz endpoint.
- Any change to the MCP stdio transport.

## Open questions

<!-- none — mechanical change -->

## Reconcile log

### 2026-08-23

Reconciled against current `main`. The premise holds unchanged and no part of this
work has been done elsewhere:

- `cmd/workbench/main.go:27` declares `--http` (env `KANBAN_HTTP`) and passes the
  raw value straight to `viz.Server.Serve`, which hands it to `http.Server{Addr:…}`
  (`internal/viz/viz.go:197-198`). A port-only `:7777` therefore binds all
  interfaces, exactly as the review flagged.
- Two additional entry points reach the same sink and are folded into scope rather
  than split out, because normalizing only `--http` would leave the documented
  local-only surface externally bound:
  - `KANBAN_HTTP` feeds the same flag default, so it inherits the same defect.
  - `--viz-only` (`main.go:71-75`) substitutes its own `:7777` fallback when
    `--http` is empty — all-interfaces even with no flag given at all.
- Normalization belongs in `internal/viz` (which has `viz_test.go`) rather than in
  `package main` (no test file), so the rule is directly testable.
- README carries six affected references (lines 53-195), including two `--http :7777`
  invocations and an MCP client config example.

Scope adjusted (the two extra entry points); nothing dropped. `trivial: true` still
holds — this stays a mechanical change with no design question outstanding.

Adjacent work observed, not captured (`auto_capture.enabled: false` — reported in
prose only): the viz handler sets `Access-Control-Allow-Origin: *`
(`internal/viz/viz.go:186`), so any web page can read the board API off loopback.
That is distinct hardening work, out of scope here, and worth its own change.
