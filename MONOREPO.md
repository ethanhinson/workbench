# Workbench v2 — monorepo layout

v2 turns this repo into a **Turborepo + pnpm monorepo**. The existing Go MCP
server is now one member (`apps/server`); new TypeScript work builds on a shared
TOON envelope foundation.

```
workbench/
  package.json  pnpm-workspace.yaml  turbo.json  tsconfig.base.json
  apps/
    server/       Go MCP server (module github.com/ethanhinson/workbench)
                  — the existing v1 board server, moved verbatim from the root
    prototyper/   @workbench/prototyper — v1 runnable review loop (HTTP+WS).
                  Wires an adapter to the browser shell; `--fake` mode drives the
                  loop with no live harness, which is the end-to-end test.
  packages/
    envelope/     @workbench/envelope — the TOON response/honesty contract
    components/   @workbench/components — Stencil web components; first set:
                  wb-chat (chat experience) + wb-annotation (prototyper)
    adapters/     @workbench/adapters — harness adapters (codex/claude/cursor),
                  one submodule per harness; import only the one you use
  docs/design/    agentic-layout.md, v2-toon-foundation-and-review.md
  skills/         methodology skills (unchanged)
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
- **components are reusable, framework-free.** Stencil web components with real
  props/state/events architecture. The first set is the prototyper's chat
  experience (`wb-chat`) and annotation surface (`wb-annotation`); they emit
  events the adapter folds into an envelope and never talk to a harness.
- **One core, many harnesses.** The components are harness-neutral; a per-harness
  adapter (`@workbench/adapters/{codex,claude,cursor}`) is the only harness-specific
  code, and each is import-isolated so a consumer pulls only what it uses.

Status: **scaffold.** `packages/envelope` and `packages/components` have real
code + tests; `packages/adapters` defines the neutral `HarnessAdapter` interface
with per-harness stubs. See the design doc for open questions and the cheapest
falsifying gate before further build.
