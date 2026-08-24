<!-- docket:backlink:start (generated — do not hand-edit) -->
> ↩ **[Change 0001 — Bind viz HTTP to 127.0.0.1 by default](https://github.com/ethanhinson/workbench/blob/docket/docs/changes/active/0001-loopback-bind-default.md)**
<!-- docket:backlink:end -->

# Plan — Bind viz HTTP to 127.0.0.1 by default (change 0001)

## Goal

A port-only listen address (`:7777`) must bind loopback only. An explicitly
host-qualified address must keep binding exactly what it names, so LAN/remote
exposure stays available as a conscious opt-in.

## Rule being implemented

Normalize the viz listen address at a single choke point:

| Input | Result | Why |
|---|---|---|
| `:7777` | `127.0.0.1:7777` | port-only ⇒ local-only default |
| `7777` | `127.0.0.1:7777` | bare port, same intent |
| `0.0.0.0:7777` | `0.0.0.0:7777` | explicit opt-in, untouched |
| `192.168.1.5:7777` | unchanged | explicit host, untouched |
| `localhost:7777` | unchanged | already local, and explicit |
| `[::]:7777` | unchanged | explicit all-interfaces v6 |
| `` (empty) | unchanged | disabled; caller decides |

The rule keys strictly on *"did the operator name a host?"* — never on whether the
named host happens to be local. Only an absent host is defaulted.

## Task 1 — `viz.NormalizeAddr` with tests

Add an exported `NormalizeAddr(addr string) string` to `internal/viz`, the package
that owns `Serve` and already has `viz_test.go`. `package main` has no test file, so
placing the rule there would make it untestable — this is why the helper lives in
`internal/viz`.

Implementation notes:

- Use `net.SplitHostPort`. A successful split with an empty host ⇒ re-join with
  `127.0.0.1`. A successful split with a non-empty host ⇒ return unchanged.
- A *failed* split is the bare-port case (`7777`) or genuine junk. Treat an
  all-digit value as a bare port and return `127.0.0.1:<port>`; return anything
  else unchanged and let `ListenAndServe` produce its own error — this helper must
  never invent an address out of a malformed one, and must never itself error.
- Empty string returns empty (the "disabled" sentinel must survive intact).

Tests: table-driven over every row above plus the empty and malformed inputs.

Verification: `go test ./internal/viz/`

## Task 2 — Apply at both call sites in `cmd/workbench/main.go`

Two entry points reach `Serve`, and both are in scope:

1. The `--viz-only` path (`main.go:71-80`), which substitutes its own `:7777`
   fallback when `--http` is empty — all-interfaces today even with no flag given.
2. The background `--http` path (`main.go:82-90`).

Normalize once per path, into a local variable, *before* both the log line and the
`Serve` call — so the logged URL and the actual bind can never disagree. This also
covers the `KANBAN_HTTP` env var for free, since it feeds the same flag.

Fix the log lines while here: both hardcode `http://localhost%s`, which prints a
misleading URL when the operator opted into `0.0.0.0`. Log the normalized address
itself.

Verification: `go build ./...`, plus a manual check that
`--http :7777` reports and binds `127.0.0.1:7777`.

## Task 3 — Documentation

Update `README.md` so documented behavior matches actual behavior. Six references
(lines 53-195), including two `--http :7777` invocations and one MCP client config
example. State the loopback default explicitly and show the `0.0.0.0` opt-in, so a
reader who *wants* LAN access knows the supported way to get it.

Verification: re-read the changed sections; no code impact.

## Out of scope

TLS/auth on the viz endpoint; any change to the MCP stdio transport; the
`Access-Control-Allow-Origin: *` header on the viz handler (noted during reconcile
as separate hardening work).

## Full-suite gate

`go build ./... && go test ./...`
