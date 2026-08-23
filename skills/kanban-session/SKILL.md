---
name: kanban-session
description: Use when the user wants a live kanban board of the work happening in this Claude Code session — driving Workbench directly over MCP so a board is started and items are created/moved/linked as the work progresses. Trigger on "start a board", "track this on the board", "show me a kanban of what we're doing", "put this on the kanban", "glue this session to the board".
---

# kanban-session — start a board and drive it live from a Claude Code session

## Overview

Workbench is wired into Claude Code as the **`workbench`** MCP server (see the
repo's `.mcp.json`). The mental model:

1. **Start a board** with `board_start` — you get back a `board_id`.
2. **Shape it** with `board_set_layout` — declare the nav tabs + views (a board
   renders nothing until it has a layout).
3. **Add work**: `item_create` (or `item_upsert` for source-keyed cards), tagging
   each with `view:`/`lane:`/`column:` labels so it lands where you want; `item_link`
   dependencies.

One database hosts **many boards**, grouped by project (a directory path) — run
several in parallel and switch between them in the UI.

**For a project that uses a methodology tool** (docket / OpenSpec / Superpowers),
don't hand-build the board — use the matching skill (**kanban-docket**,
**kanban-openspec**, **kanban-superpowers**), or **kanban-methodologies** to pick.
This `kanban-session` skill is for **ad-hoc session work** you shape by hand.

## Prerequisites

The `workbench` MCP server is available (globally in Claude Code; see `/mcp`). It
serves the board UI at <http://localhost:7777> and stores boards in a shared
database. If `/mcp` doesn't list `workbench`, install and register it per the
Workbench README's onboarding, then reload.

## The tools (all under the `workbench` server)

**Every item/board tool takes an explicit `board_id`** — there is no hidden
"active board." Call `board_start` first and pass the returned id to the rest.
(The item-addressed tools — move/set_spec/set_blocked/label/comment — take
`item_id` and resolve the board from the item, so they don't need `board_id`.)

**Boards are grouped by project** (a directory path; defaults to the working
directory). Since the server is shared across projects, pass `project` to
`board_start` — set it to your project root (e.g. `$CLAUDE_PROJECT_DIR`) — so this
session's boards are grouped under it rather than the server's cwd.

| Tool | Use it to |
|---|---|
| `board_start` | **Start here.** Create/select a board by `(project, name)` → returns `board_id`. Optional `project` (dir path, defaults to cwd) + `profile` (sdd\|scrum\|kanban). Idempotent per (project, name). |
| `board_list` | List boards (id, name, project, profile, item count); pass `project` to list just one project's boards. |
| `board_delete` | Delete a board (`board_id`) and everything on it — irreversible. Clean up throwaway/finished boards. |
| `board_rename` | Rename a board (`board_id`, `name`) — names are unique within a project. |
| `board_set_project` | Move a board (`board_id`, `project`) to a different project — e.g. re-home an older board under its repo. |
| `board_set_layout` | **Shape the board:** declare `nav` tabs + `views` (type `list\|lanes\|board\|doc`). A board renders nothing until this is set. |
| `board_get_layout` | Read the current layout to tweak it. |
| `board_view` | See one board (`board_id`) as columns × lanes (text form). |
| `item_create` | Add an item (`board_id` + `epic\|story\|task\|bug\|spike`); tag `view:`/`lane:`/`column:` + `content`. Nest with `parent_id`. |
| `item_upsert` | Create-or-update a card by `ext_key` (idempotent) with `content` + placement labels — the hydration primitive. |
| `item_set_content` | Replace a card's `content` (the doc markdown a `doc` view renders). |
| `item_link` | Link two items (`board_id`, `from_id`, `to_id`, `kind`: depends_on\|related\|discovered_from). Flat, not nested. |
| `item_move` | Move an item to a profile column: `backlog\|specifying\|specd\|in_progress\|review\|done` (orthogonal to view placement). |
| `item_set_spec` | Set spec ref + status `missing\|draft\|approved` (SDD heartbeat). |
| `item_set_blocked` | Flag/unflag blocked, with a reason. |
| `item_label` | Namespaced labels incl. `view: lane: column:` (placement) + `type: priority: spec: area:` as `ns:value`. |
| `item_comment` | Append to an item's activity log. |
| `items_list` | List a board's items with filters. |
| `board_export` | The full renderer-agnostic Snapshot (incl. layout) for a custom UI. |

The **sdd** profile enforces: nothing leaves `specd` until its
`spec_status=approved`, and blocked items can't advance — the server rejects
invalid moves, so you can't drift the board out of policy.

## Working the board during a session

1. **Start it.** `board_start` with a name for what you're building (e.g. the
   deliverable, or the session topic) and `project` set to your project root
   (`$CLAUDE_PROJECT_DIR`) so it's grouped with this project. Keep the returned
   `board_id`.
2. **Frame the work.** `item_create` a `story` (or `epic` for a big slice) on that
   `board_id`; it lands in `backlog`. Break out `task` children with `parent_id`.
3. **Wire dependencies.** `item_link` tasks that block each other (`depends_on`) or
   that you discovered while doing another (`discovered_from`).
4. **Spec it.** `item_move` → `specifying`; `item_set_spec` with the spec path and
   `draft`, then `approved` once settled → move → `specd`.
5. **Build it.** `item_move` → `in_progress`. Flag `item_set_blocked` with a reason
   if something stalls.
6. **Close it.** `item_move` → `review`, then `done`. Drop an `item_comment`
   summarizing what shipped.

Keep it lightweight — track real units of work, not every micro-step. Use
`board_view` first if you're unsure of current state.

## Notes

- The board is **live over SSE** — the open browser tab updates on every tool call,
  no refresh. The UI has a board picker when the db holds more than one board
  (switch with `?board=<id>` in the URL).
- One `.kanban/session.db` hosts many boards; each `board_start` name is a distinct
  board. Delete the db to start over.
- Nothing here is Claude-specific in the server; it's the same MCP surface any
  harness would drive. This skill just names the session-driving convention.
