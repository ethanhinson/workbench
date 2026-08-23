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
- **The board's shape is agent-authored.** A methodology (docket / OpenSpec /
  Superpowers) is a **skill** that declares the board's layout and projects the
  tool's artifacts onto it — see [Agentic layout](#agentic-layout--methodologies-are-skills).

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
plan, **layout**, items (with labels + `content`), links, and stats). That Snapshot
is the seam — the bundled SPA, an on-demand generated component, or a static export
all consume the same shape, reading no files (all content is in the snapshot).

- **`GET /api/board`** — the Snapshot contract (CORS-open, so external renderers can fetch it).
- **`GET /api/stream`** — the same Snapshot pushed over **SSE** on every store mutation (no polling).
- **`GET /api/item/{id}`** — full card detail: the item (with its `content`) + bidirectional dependencies.
- **`GET /`** — a zero-build reference SPA that renders whatever `layout` the board declares.
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

## Agentic layout — methodologies are skills

A board's shape is **data the agent authors**, not hard-coded. `board_set_layout`
declares the **nav** tabs and their **views**; each view is one of four types:

- `list` — a flat list of cards
- `lanes` — swimlanes where **lanes are status** (To Do/Doing/Done, or a pipeline)
- `board` — vertical swimlanes only
- `doc` — a rendered-markdown reader over cards' `content`

Placement is **explicit labels**: an item's `view:` (which nav view) and `lane:`
(which status lane) decide where it appears; a **`group:` label** shows an
epic/grouping (a change, plan, or type) as a color-coded chip on the card — a
glanceable grouping, not an axis. The renderer just buckets by tag — no Go
placement logic. A board with no layout renders an empty state until a skill (or
`board_set_layout`) shapes it.

**Content lives on the card, not the filesystem.** The agent puts a spec/ADR's
markdown into the item's `content` field (via `item_upsert` / `item_set_content`);
the `doc` view renders it. The server reads no files — there is no `--repo-root`.

A **methodology is a skill** (`skills/kanban-<tool>/SKILL.md`): a prompt that reads
a tool's files, declares a tool-idiomatic layout, and upserts cards (keyed by a
stable `ext_key`) as the agent works. Shipped:

| Skill | For a repo with… | Builds |
|---|---|---|
| **kanban-docket** | `.docket/docs/changes/` | Backlog / In-Flight / ADRs / Done |
| **kanban-openspec** | `openspec/changes/` + `specs/` | Proposals / Tasks / Specs / Archive |
| **kanban-superpowers** | `docs/superpowers/{specs,plans}` | Plans / In-Progress / Specs / Reviews |
| **kanban-session** | (ad-hoc) | a board you shape by hand |
| **kanban-methodologies** | — | index skill: picks the right one |

Hydration is a **rhythm**: whenever the agent touches a source artifact, it
`item_upsert`s that card (same `ext_key` → idempotent update). The board is a live
projection of the session; a full re-hydrate is just that loop over every artifact.

## Tools

Board-addressed tools take a **`board_id`** (from `board_start`). Item-addressed
tools take an **`item_id`** and resolve the board from it.

| Tool | Purpose |
|------|---------|
| `board_start` | **Start here.** Create/select a board by `(project, name)` → `board_id` (idempotent). Optional `project` defaults to the server cwd |
| `board_list` | List boards (id, name, project, profile, item count); optional `project` filter |
| `board_set_layout` | **Shape the board:** declare `nav` tabs + `views` (`list\|lanes\|board\|doc`). Required before anything renders |
| `board_get_layout` | Read the current layout to tweak it |
| `board_delete` | Delete a board and everything on it — irreversible |
| `board_rename` | Rename a board (names unique within its project) |
| `board_set_project` | Move a board to a different project |
| `board_view` | Render one board (columns × lanes) as text |
| `item_create` | Create an epic/story/task/bug/spike; tag `view:`/`lane:`/`column:` + `content` |
| `item_upsert` | Create-or-update a card by `ext_key` (idempotent) — the hydration primitive |
| `item_set_content` | Replace a card's `content` (the doc markdown a `doc` view renders) |
| `item_link` | Link two items: `depends_on` \| `related` \| `discovered_from` |
| `item_move` | Move to a profile column (respects WIP/gates) |
| `item_set_spec` | Set `spec_ref` + `spec_status` |
| `item_set_blocked` | Flag/unflag blocked with a reason |
| `item_label` | Add validated `ns:value` labels (incl. `view:`/`lane:`/`column:`) |
| `item_comment` | Append to an item's activity log |
| `lane_configure` | Create/ensure a swim lane on a board |
| `items_list` | List a board's items with filters |
| `board_export` | Export the renderer-agnostic Snapshot (incl. layout) for a custom UI |

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

Env var equivalents: `KANBAN_DB`, `KANBAN_PLAN`, `KANBAN_AGENT`, `KANBAN_PROJECT`.

## Test

```sh
go test ./...
```
