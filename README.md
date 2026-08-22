# kanban-mcp

An MCP server that gives a coding agent **kanban boards** for spec-driven
development (SDD / Spec-DD) — a single pane of glass over the work discussed and
done in a run.

The agent **starts a board** at runtime and drives it live:

```
board_start "Auth work"   → board_id            an agent creates/selects a board
 ├─ item_create story/task/…                     add work to it (board_id)
 ├─ item_link  from → to  (depends_on|related)   wire dependencies (flat, not nested)
 └─ item_move  → specifying → specd → …          drive it as the session progresses
```

- **Many boards per database, grouped by project.** One SQLite file hosts many
  boards; each board belongs to a **project** (by default the working directory, so
  "a project is a directory"). `board_start` creates or selects a board by
  `(project, name)` — idempotent — so the same board name can exist under different
  projects, and the UI groups boards by project. **Every tool names its `board_id`
  explicitly — there is no hidden "active board."**
- **The board owns live in-flight work.** Backlog, done, and ADRs are **decoupled
  read-only inputs**, projected in from an external source (docket today) via a
  pluggable `SourceProvider` — see [Sources](#external-sources-decoupled-inputs).

The board is **columns × swim lanes**. Columns are the workflow; lanes are
configurable and default to **one per agent**, so several agents can share one
board while each owns a lane that rolls up into it. Items can still nest
(`parent_id`: epic → story → task), but relationships are expressed as
first-class **links**, not containment.

## Design choices

- **SQLite, many boards per file.** WAL + `busy_timeout` so concurrent agents can
  read/write. Set the file with `--db`; create/select boards with `board_start`.
- **Runtime boards, explicit ids.** No launch-time plan binding is required —
  `board_start` returns a `board_id` you pass to every other tool. Each agent
  connects with its own `--agent` id and gets a swim lane automatically.
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
plan, columns, lanes, items, links, per-view placements, and stats). That Snapshot
is the seam — the bundled SPA, an on-demand generated component, or a static export
all consume the same shape.

- **`GET /api/board`** — the Snapshot contract (CORS-open, so external renderers can fetch it).
- **`GET /api/stream`** — the same Snapshot pushed over **SSE** on every store mutation (no polling).
- **`GET /api/item/{id}`** — full card detail: bidirectional dependencies + rendered spec/plan content.
- **`GET /`** — a zero-build reference SPA (embedded, vanilla JS, live over SSE via `EventSource`).
- **MCP `board_export`** — hands an agent the same Snapshot, to drive on-demand UI generation.

### Live updates: SSE, read-only board

Every store mutation bumps a revision on an in-process broker; `GET /api/stream`
turns that into Server-Sent Events. The board is **read-only from the UI's side**
(the agent writes through MCP), so SSE — server→client push over plain HTTP with
auto-reconnect — is the right fit; WebSockets would add a duplex channel nothing
uses.

> A terminal UI (TUI) client is deliberately out of scope for now — the Snapshot +
> SSE contract makes one easy to add later without server changes.

The UI shows a **board picker** when the db holds more than one board; a request
selects its board with `?board=<id>` (defaulting to the board seeded at startup).

```sh
# browse boards in a db (no agent needed)
./kanban-mcp --db ./runbook.db --viz-only --http :7777
# or serve the UI alongside the MCP stdio server
./kanban-mcp --db ./runbook.db --agent alice --http :7777
```

## External sources (decoupled inputs)

The board process owns the agent's **live in-flight work**. Everything else —
backlog, done, ADRs — is a **read-only projection** from an external source of
truth, behind a pluggable `SourceProvider` seam (`internal/source`). Which source
refreshes, and when, is decoupled from the running board: an agent, a CLI flag, or
a cron triggers a sync; the board never owns the source.

**docket** is the first provider. [docket](https://github.com/ethanhinson) tracks
work as markdown change manifests (and ADRs) on the repo's metadata branch. Because
that's just markdown-on-a-branch, kanban-mcp reads it directly — no docket tooling
or specific agent required.

```sh
# one-shot projection onto the seeded board (idempotent; re-run to refresh)
kanban-mcp --db /tmp/fuse-board.db --plan "Fuse Backlog" --profile docket \
  --source docket --docs-dir ~/dev/fuse/.docket/docs

# then browse it
kanban-mcp --db /tmp/fuse-board.db --viz-only --http :7777
```

Or from an agent: call **`source_sync`** with
`{ "board_id": "<id>", "source": "docket", "docs_dir": "<repo>/.docket/docs" }`.

If the metadata branch isn't checked out to a worktree, export it read-only first:
`git -C <repo> archive docket docs | tar -x -C "$TMP"` and point `--docs-dir` at
`$TMP/docs`. The **`kanban-docket-sync` skill** (`skills/`) automates this resolution.

Mapping: change → card (`docket:<id>`), `status` → column, `type` → swim lane,
`priority` → `p0..p3`, spec/plan presence → spec status, `blocked_by` → blocked flag,
`depends_on`/`discovered_from`/`related` → first-class links. ADRs project as
reference cards (`adr:<id>`) in an `adr` lane, linked to the change they came from.
The board is read-only over the source — the source stays the source of truth.

## Tools

Board-addressed tools take a **`board_id`** (from `board_start`). Item-addressed
tools take an **`item_id`** and resolve the board from it.

| Tool | Purpose |
|------|---------|
| `board_start` | **Start here.** Create/select a board by `(project, name)` → returns `board_id` (idempotent). Optional `project` (a dir path) defaults to the server's working directory |
| `board_list` | List boards (id, name, project, profile, item count); optional `project` filter |
| `board_delete` | Delete a board and everything on it (items, links, labels, comments) — irreversible |
| `board_rename` | Rename a board (names are unique within its project) |
| `board_set_project` | Move a board to a different project (a directory path) |
| `board_view` | Render one board (columns × lanes) — the single pane of glass |
| `item_create` | Create an epic/story/task/bug/spike on a board; nest via `parent_id` |
| `item_link` | Link two items: `depends_on` \| `related` \| `discovered_from` (flat, not nested) |
| `item_move` | Move to a column (and optionally a lane); respects WIP limits |
| `item_set_spec` | Set `spec_ref` + `spec_status` for SDD tracking |
| `item_set_blocked` | Flag/unflag blocked with a reason |
| `item_label` | Add validated `ns:value` labels |
| `item_comment` | Append to an item's activity log |
| `lane_configure` | Create/ensure a swim lane on a board |
| `items_list` | List a board's items with filters (column/lane/parent/kind) |
| `board_export` | Export the renderer-agnostic Snapshot for on-demand/custom UI |
| `source_sync` | Project an external source (docket) onto a board, read-only + idempotent |

## Run

```sh
go build -o kanban-mcp ./cmd/kanban-mcp
./kanban-mcp --db ./runbook.db --agent alice
```

An agent then calls `board_start` to create/select a board. Boards default to the
project named by `--project` (or the server's working directory if unset), so with
a global/shared db you can pass your project root — e.g. `board_start` with
`project: $CLAUDE_PROJECT_DIR` — to keep each project's boards grouped. (`--plan`
still seeds a default board for single-board / back-compat use, but isn't required.)

Register with an MCP client (e.g. Claude Code `.mcp.json`):

```json
{
  "mcpServers": {
    "kanban": {
      "command": "/path/to/kanban-mcp",
      "args": ["--db", "/path/to/runbook.db", "--agent", "alice", "--http", ":7777"]
    }
  }
}
```

Env var equivalents: `KANBAN_DB`, `KANBAN_PLAN`, `KANBAN_AGENT`, `KANBAN_PROJECT`, `KANBAN_DOCS_DIR`.

## Test

```sh
go test ./...
```
