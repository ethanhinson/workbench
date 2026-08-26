# Workbench

Workbench is a live kanban board for your coding agent. It runs as an MCP server,
so an agent (Claude Code, or anything that speaks MCP) can start a board, shape it,
and keep it current as it works. You open the board in a browser and watch the work
move in real time.

The idea is simple. Your project already tracks work somewhere: docket change
manifests, OpenSpec proposals and tasks, Superpowers plans, or just the tasks in
this session. Workbench turns any of those into a board the agent drives, without
you copying anything by hand.

## What makes it different

**The agent shapes the board, not the code.** A board has no fixed columns. The
agent declares a layout (which tabs, which swim lanes, which views) with one tool
call, and tags each card with where it belongs. So an OpenSpec board looks like
OpenSpec, a docket board looks like docket, and both are drawn by the same renderer.

**Methodologies are skills, not plugins.** Support for docket, OpenSpec, or
Superpowers is a prompt (a skill) that reads the tool's files and drives Workbench's
tools. Adding a new methodology means writing one skill, not touching the server.

**It stays live as a byproduct of the work.** There is no separate sync step. When
the agent edits a spec or checks off a task, it upserts that card in the same beat,
so the board is always as fresh as the session.

**Many boards, grouped by project.** One database holds many boards. Each board
belongs to a project (a directory by default), and the sidebar groups them, so one
Workbench serves every repo you touch.

## Quick look

```
board_start   "auth work"           -> a board_id (idempotent, project-scoped)
board_set_layout   { nav, views }    -> tabs + swim lanes; the board's whole shape
item_upsert   { ext_key, content,    -> a card, keyed for idempotent re-runs,
                lane: doing,            placed by its labels, carrying the doc it
                group: auth }           renders
```

Lanes are status (To Do, Doing, Done, or a pipeline). The epic or group a card
belongs to (a change, a plan, a type) shows as a colored chip, not another axis.

## Onboarding

Workbench has two layers. The server gives an agent the tools; a skill teaches the
agent how to use them for your methodology.

### 1. Build and install the binary

```sh
git clone https://github.com/ethanhinson/workbench
cd workbench
go build -o ~/.local/bin/workbench ./cmd/workbench
```

That produces a single static binary (no CGO, SQLite is bundled). Make sure
`~/.local/bin` is on your `PATH`.

### 2. Register it as a global MCP server (Claude Code)

One shared server, available in every project:

```sh
mkdir -p ~/.workbench
claude mcp add workbench --scope user -- \
  workbench --db ~/.workbench/boards.db --agent claude --http :7777
```

`--http :7777` serves the browser board at <http://localhost:7777>. The one
database holds every project's boards; the agent names each board's project at
`board_start`, so nothing collides.

Confirm it connected:

```sh
claude mcp list        # workbench: ... - Connected
```

### 3. Install the skills your harness needs

The skills are what make an agent actually build and maintain a board. Copy the
ones you want into your harness's skills directory. For Claude Code that is
`.claude/skills/` in a repo (project scope) or `~/.claude/skills/` (global):

```sh
# global, so every project can use them:
cp -r skills/kanban-* ~/.claude/skills/
```

The skills:

| Skill | Use it when the repo has | Builds |
|---|---|---|
| `kanban-methodologies` | (any) | An index that detects the repo's tool and points at the right skill |
| `kanban-docket` | `.docket/docs/changes/` | Backlog, In Flight, ADRs, Done |
| `kanban-openspec` | `openspec/changes/` and `specs/` | Proposals, Tasks, Specs, Archive |
| `kanban-superpowers` | `docs/superpowers/{specs,plans}` | Plans, In Progress, Specs, Reviews |
| `kanban-session` | (ad hoc) | A board you shape by hand as you work |

### 4. Use it

In a repo, tell your agent what you want, for example "put this project's docket
backlog on a board" or "start a board for this session". The agent invokes the
matching skill, starts a project-scoped board, sets its layout, and fills it. Open
<http://localhost:7777> and pick the board from the sidebar. It updates live over
Server-Sent Events as the agent works.

## How it works

A board's shape is data the agent authors, not fixed in the code. `board_set_layout`
declares the nav tabs and their views. A view is one of four types:

- `list` a flat list of cards
- `lanes` swim lanes where lanes are status (To Do, Doing, Done, or a pipeline)
- `board` vertical swim lanes only
- `doc` a rendered-markdown reader over each card's `content`

Placement is column-driven. Each view *owns* a set of the board's real columns, and
an item's nav view is derived from its `column_key` — so moving a card (via
`item_move`, or a methodology adapter) instantly changes where it renders, with no
separate labels to keep in sync. Within a lanes/board view, cards swimlane by their
owned column by default. `group:` still shows an epic or grouping (a change, a plan,
a type) as a color-coded chip. A board with no layout renders an empty state until a
skill or `board_set_layout` shapes it.

Content lives on the card, not the filesystem. The agent puts a spec or ADR's
markdown into the item's `content` field, and the `doc` view renders it. The server
never reads files, so any renderer works anywhere the snapshot reaches.

A methodology is a skill under `skills/kanban-<tool>/SKILL.md`: a prompt that reads a
tool's files, declares a tool-idiomatic layout, and upserts cards keyed by a stable
`ext_key`. Hydration is a rhythm rather than a batch job: whenever the agent touches
a source artifact it upserts that card with the same key, which only updates, so the
board stays as fresh as the work.

The board is exposed as a renderer-agnostic Snapshot (JSON: plan, layout, items with
labels and content, links, stats), served at `GET /api/board` and pushed over
Server-Sent Events at `GET /api/stream` on every change. The bundled single-page app
is one consumer; the same contract drives a custom renderer or a static export.


## Tools

Board-addressed tools take a **`board_id`** (from `board_start`). Item-addressed
tools take an **`item_id`** and resolve the board from it.

| Tool | Purpose |
|------|---------|
| `board_start` | **Start here.** Create/select a board by `(project, name)` → `board_id` (idempotent). Optional `project` defaults to the server cwd |
| `board_list` | List boards (id, name, project, profile, item count); optional `project` filter |
| `board_set_layout` | **Shape the board:** declare `nav` tabs + `views` (`list\|lanes\|board\|doc`). Required before anything renders |
| `board_get_layout` | Read the current layout to tweak it |
| `board_delete` | Delete a board and everything on it (irreversible) |
| `board_rename` | Rename a board (names unique within its project) |
| `board_set_project` | Move a board to a different project |
| `board_view` | Render one board (columns × lanes) as text |
| `item_create` | Create an epic/story/task/bug/spike; tag `view:`/`lane:`/`column:` + `content` |
| `item_upsert` | Create-or-update a card by `ext_key` (idempotent), the hydration primitive |
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
go build -o ~/.local/bin/workbench ./cmd/workbench
workbench --db ./runbook.db --agent alice
```

An agent then calls `board_start` to create/select a board. Boards default to the
project named by `--project` (or the server's working directory if unset), so with
a global/shared db you can pass your project root, e.g. `board_start` with
`project: $CLAUDE_PROJECT_DIR`, to keep each project's boards grouped. (`--plan`
still seeds a default board for single-board / back-compat use, but isn't required.)

Register with an MCP client (e.g. Claude Code `.mcp.json`):

```json
{
  "mcpServers": {
    "kanban": {
      "command": "/path/to/workbench",
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

## License

MIT. See [LICENSE](LICENSE).
