---
id: 1
slug: loopback-bind-default
title: Bind viz HTTP to 127.0.0.1 by default
status: proposed
priority: high
type: fix
created: 2026-08-23
updated: 2026-08-23
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

A competitive-analysis review flagged that the documented `--http :7777` binds on
all interfaces in Go, while the tool is described and intended as a local-only
developer surface. Actual network behavior should match that intent: a bare port
should listen on loopback only, with explicit opt-in for LAN/remote.

## What changes

- When `--http` is given a port-only value (`:7777`), default the bind host to `127.0.0.1`.
- Preserve explicit opt-in: `--http 0.0.0.0:7777` (or a named host) still binds externally.
- Update README examples and any UI "localhost" language to match actual behavior.

## Out of scope

- TLS / auth on the viz endpoint.
- Any change to the MCP stdio transport.

## Open questions

<!-- none — mechanical change -->

## Reconcile log
