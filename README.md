# kanban-mcp

An MCP server that gives a coding agent a **kanban board** for spec-driven
development (SDD / Spec-DD) — a single pane of glass over the work discussed and
done in a run.

Work nests the way stories and epics do:

```
Plan            one shared board  (= one SQLite file)
 └─ Epic        a large slice of the plan (often a spec doc)
     └─ Story   one deliverable, ~one PR
         └─ Task a checklist item
```

The board is **columns × swim lanes**. Columns are the workflow; lanes are
configurable and default to **one per agent**, so several agents can share one
plan while each owns a lane that rolls up into it.

## Design choices

- **SQLite, one file per plan.** WAL + `busy_timeout` so concurrent agents can
  read/write. Set with `--db`.
- **Nested & shared.** A single plan (`--plan`) is shared; each agent connects
  with its own `--agent` id and gets a swim lane automatically.
- **SDD-opinionated, overridable columns:**
  `Backlog → Specifying → Spec'd → In Progress → Review → Done`.
  `Blocked` is a flag on an item, not a column — a blocked item keeps its stage.
- **Consistent labels.** Namespaced and enum-validated so agents can't drift the
  taxonomy:
  - `type:` epic | story | task | bug | spike
  - `priority:` p0 | p1 | p2 | p3
  - `spec:` missing | draft | approved
  - `stage:` (kept in sync with the plan's columns)
  - `agent:` / `area:` (open — any slug)
- **Spec tracking is first-class.** Every item carries a `spec_ref` and
  `spec_status` (the SDD heartbeat), settable via `item_set_spec`.
- **Audit log.** Every create/move/label/block/comment is appended to an event
  log, so an agent can reconstruct "what's discussed" in a run.

## Methodology profiles — how the workflow influences swim lanes

Columns and swim lanes are **orthogonal** storage. A **profile** is the thing that
binds meaning to both axes and enforces how they interact. Selected on first init
with `--profile` (or `KANBAN_PROFILE`); overridable per plan.

A profile declares:

- **columns** — the workflow stages
- **lane_dimension** — *what a lane means*: `agent` | `epic` | `class_of_service` | custom
- **policies** — the rules the server enforces on every move/create:
  - `column_gates` — preconditions to *leave* a column (e.g. `spec_status=approved`)
  - `transitions` — allowed column→column moves
  - `lane_wip` — per-lane WIP caps
  - `exempt_lanes` — lanes that bypass gates/WIP

Built-in presets show three different answers to "how does the workflow touch lanes":

| Profile | lane = | The coupling |
|---|---|---|
| **sdd** | agent | Nothing leaves `Spec'd` until `spec_status=approved`, and nothing advances while blocked — enforced **across every lane**. The workflow constrains all lanes. |
| **scrum** | epic | Strict left-to-right `transitions`; stories flow within their epic's lane. |
| **kanban** | class of service | `standard` lane has WIP 5; the `expedite` lane is **exempt** — here a *lane* overrides the *workflow*. |

Invalid moves are rejected by the server, so the board can't drift out of policy.

## Visualization — a pluggable UI layer

The board is exposed as a renderer-agnostic **Snapshot** (schema-versioned JSON:
plan, columns, lanes, items, a precomputed cell grid, and stats). That Snapshot is
the seam — the bundled SPA, an on-demand generated component, a TUI, or a static
export all consume the same shape.

- **`GET /api/board`** — the Snapshot contract (CORS-open, so external renderers can fetch it).
- **`GET /`** — a zero-build reference SPA (server-side embedded, vanilla JS, auto-refresh).
- **MCP `board_export`** — hands an agent the same Snapshot, to drive on-demand UI generation.

```sh
# browse a board (no agent needed)
./kanban-mcp --db ./runbook.db --plan "Runbook" --viz-only --http :7777
# or serve the UI alongside the MCP stdio server
./kanban-mcp --db ./runbook.db --plan "Runbook" --agent alice --http :7777
```

## Importing a docket backlog (works with any harness)

[docket](https://github.com/ethanhinson) tracks work as markdown change manifests on
the repo's metadata branch. Because that's just markdown-on-a-branch, kanban-mcp can
read it directly and render it — no docket tooling or specific agent required.

```sh
# one-shot import (idempotent; re-run to refresh)
kanban-mcp --db /tmp/fuse-board.db --plan "Fuse Backlog" \
  --profile docket --docket-sync ~/dev/fuse/.docket/docs

# then browse it
kanban-mcp --db /tmp/fuse-board.db --plan "Fuse Backlog" --viz-only --http :7777
```

Or from an agent: call the **`docket_sync`** MCP tool with `{ "docs_dir": "<repo>/.docket/docs" }`.

If the metadata branch isn't checked out to a worktree, export it read-only first:
`git -C <repo> archive docket docs | tar -x -C "$TMP"` and point `--docket-sync` at
`$TMP/docs`. The **`kanban-docket-sync` skill** (`skills/`) automates this resolution.

Mapping: change → card (`docket:<id>`), `status` → column, `type` → swim lane,
`priority` → `p0..p3`, spec/plan presence → spec status, `blocked_by` → blocked flag,
`discovered_from`/`depends_on` → nesting. The board is read-only over docket — docket
stays the source of truth.

## Tools

| Tool | Purpose |
|------|---------|
| `board_view` | Render the whole board (columns × lanes) — the single pane of glass |
| `item_create` | Create an epic/story/task/bug/spike; nest via `parent_id` |
| `item_move` | Move to a column (and optionally a lane); respects WIP limits |
| `item_set_spec` | Set `spec_ref` + `spec_status` for SDD tracking |
| `item_set_blocked` | Flag/unflag blocked with a reason |
| `item_label` | Add validated `ns:value` labels |
| `item_comment` | Append to an item's activity log |
| `lane_configure` | Create/ensure a swim lane |
| `items_list` | List items with filters (column/lane/parent/kind) |
| `board_export` | Export the renderer-agnostic Snapshot for on-demand/custom UI |
| `docket_sync` | Import a docket backlog (markdown-on-a-branch) into the board, idempotently |

## Run

```sh
go build -o kanban-mcp ./cmd/kanban-mcp
./kanban-mcp --db ./runbook.db --plan "Runbook" --agent alice
```

Register with an MCP client (e.g. Claude Code `.mcp.json`):

```json
{
  "mcpServers": {
    "kanban": {
      "command": "/path/to/kanban-mcp",
      "args": ["--db", "/path/to/runbook.db", "--plan", "Runbook", "--agent", "alice"]
    }
  }
}
```

Env var equivalents: `KANBAN_DB`, `KANBAN_PLAN`, `KANBAN_AGENT`.

## Test

```sh
go test ./...
```
