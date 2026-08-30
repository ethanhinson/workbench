# Workbench v2 — monorepo layout

v2 turns this repo into a **Turborepo + pnpm monorepo**. The existing Go MCP
server is now one member (`apps/server`); new TypeScript work builds on a shared
TOON envelope foundation.

```
workbench/
  package.json  pnpm-workspace.yaml  turbo.json  tsconfig.base.json
  apps/
    server/         Go MCP server (module github.com/ethanhinson/workbench)
                    — the existing v1 board server, moved verbatim from the root
    review-bridge/  local bridge: browser <-> harness loop (TS)
  packages/
    envelope/       @workbench/envelope — the TOON response/honesty contract
    review-chrome/  in-house inspector + annotation + option-picker chrome
    review-ui/      the 80/20 browser shell (prototype pane + chat pane)
  docs/design/      agentic-layout.md, v2-toon-foundation-and-review.md
  skills/           methodology skills (unchanged)
```

## Working in it

```sh
pnpm install          # install JS workspaces
pnpm build            # turbo build across packages/apps
pnpm test             # turbo test

# the Go server:
cd apps/server && go build ./... && go test ./...
```

The Go module path is unchanged (`github.com/ethanhinson/workbench`), so its
internal imports (`.../internal/...`) resolve exactly as before — only the
module's location in the tree moved.

## Why this shape

- **envelope is the foundation.** Every agent-facing tool renders the same
  honesty contract (coverage, staleness, aggregates, provenance). See
  `packages/envelope/README.md`.
- **The browser review tool is the first consumer.** Agent renders an HTML
  prototype; a human clicks/annotates/picks options; structured feedback flows
  back into the running agent turn as an envelope. See
  `docs/design/v2-toon-foundation-and-review.md`.
- **One core, many harnesses.** The browser + bridge is harness-neutral; a thin
  adapter (codex / claude / cursor) is the only per-harness code.

Status: **scaffold.** `packages/envelope` has real code + tests; the
review-* packages and the bridge are structured placeholders with their
contracts sketched. See the design doc for open questions and the cheapest
falsifying gate before further build.
