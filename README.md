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
