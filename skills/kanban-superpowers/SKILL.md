---
name: kanban-superpowers
description: Use whenever you are working with a Superpowers SDD project (a `docs/superpowers/` directory with plans/specs, or a `.superpowers/` state dir) — reviewing plans, tracking task execution and reviews, or reporting progress. Do not just read the files and answer; project the plans, tasks, specs, and reviews onto a live Workbench board so the human can see execution status, then keep it current as work lands. Triggers include "review the superpowers plans", "how's the plan going", "what's left to do", "status of the plan", as well as explicit "show the superpowers board".
---

# kanban-superpowers — project a Superpowers workflow onto a kanban board

## What this is

[Superpowers](https://github.com/obra/superpowers) drives spec-driven development
through **brainstorm → spec → plan → execute**, storing artifacts at:
- `docs/superpowers/specs/YYYY-MM-DD-<name>.md` — the settled specs
- `docs/superpowers/plans/YYYY-MM-DD-<name>.md` — bite-sized implementation plans
- `.superpowers/` — brainstorm/execution state; per-task briefs/reports and review
  diffs (`task-N-brief.md`, `task-N-report.md`, `review-*.diff`, `progress.md`)

This skill teaches you to read those and build a **Superpowers-idiomatic kanban
board** via Workbench — you read the files, upsert the cards, declare the layout.

## Prerequisites

The `kanban` MCP server is available (see `/mcp`); viz at <http://localhost:7777>.

## Step 1 — locate the Superpowers artifacts

Confirm `<repo>/docs/superpowers/` exists (specs/ and/or plans/). Note any
`.superpowers/sdd/` execution state (task reports, review diffs, progress.md).

## Step 2 — start the board

```
board_start { name: "<repo>: superpowers", project: "<repo-abs-path>", profile: "superpowers" }
```
**Pass `profile: "superpowers"`** — it binds the task-progress columns and the plan
lane dimension. Omitting it falls back to the `sdd` profile, which mislabels the
board (sdd lifecycle columns + a stray `spec:missing` badge on every card).

## Step 3 — design the layout, then set it

**Placement is column-driven.** The superpowers profile's real columns are `todo`,
`doing`, `done`. Each view *owns* a subset of those columns (via its
`columns: [{key, label}]`), and a card's nav view is derived from its `column_key` —
whichever view owns that column. Propose this (adjust interactively — see
[Interactive setup](#interactive-setup)):

```
board_set_layout { board_id, layout: {
  nav: [
    { id: "plans",    label: "Plans",    view: "plans" },
    { id: "progress", label: "In Progress", view: "progress" },
    { id: "specs",    label: "Specs",    view: "specs" },
    { id: "reviews",  label: "Reviews",  view: "reviews" }
  ],
  views: {
    // Plans own todo; each plan card is a story that sits there.
    "plans":    { type: "list",
                  columns: [{key:"todo",label:"Plans"}] },
    // Progress owns todo/doing/done; task cards swimlane by their owned column_key.
    "progress": { type: "lanes", group_by: "group",
                  columns: [{key:"todo",label:"To Do"},{key:"doing",label:"Doing"},{key:"done",label:"Done"}] },
    "specs":    { type: "doc" },
    "reviews":  { type: "list",
                  columns: [{key:"done",label:"Reviews"}] }
  }
}}
```

**Task cards swimlane by their owned `column_key`.** The `progress` view reads like a
kanban board (To Do / Doing / Done) because it owns those three columns, and each
task card carries a **`group:<plan>` chip** (color-coded) so you see which plan a
task belongs to. This layout is **fixed** — no per-plan discovery needed before
`board_set_layout`.

## Step 4 — hydrate the cards (upsert by ext_key)

**`<name>` = the plan slug** — the filename with the `YYYY-MM-DD-` date prefix
stripped (e.g. `2026-07-12-m1-walkable-slice.md` → `m1-walkable-slice`). Use it
consistently for the plan's `ext_key`, its lane key, and its tasks' keys.

For each **plan** `docs/superpowers/plans/<date>-<name>.md`, `item_upsert`
`ext_key: "sp:plan:<name>"` with `column_key: "todo"` (owned by the `plans` view),
`content:` the plan markdown, `spec_ref:` the path.

**Tasks — how Superpowers plans actually structure them:** a plan's tasks are
`### Task N: <title>` headings (sometimes sub-numbered `### Task N.M:`), each with
bite-sized `- [ ] Step …` items underneath. Parse the `### Task N` headings — NOT
the step checkboxes — one card per task. **Done-ness is authoritative in
`.superpowers/sdd/progress.md`**, a ledger of `Task N: complete …` lines; treat a
task as done if progress.md marks it complete (a `task-N-report.md` file is a
secondary hint). For each task, `item_upsert`
`ext_key: "sp:plan:<name>:task:<N>"`, carrying its `column_key` (the `progress`
view owns todo/doing/done):

| Task state (from progress.md) | `column_key` |
|---|---|
| marked complete | `done` |
| in flight | `doing` |
| not started | `todo` |

Also set **`group:<name>`** so the card shows its plan as a colored chip;
`item_link depends_on` the plan card; pull `task-N-report.md` (if present) into
`content`.

> Multiple plans can coexist under one `.superpowers/sdd/`. `progress.md` and the
> `task-N-*.md` files belong to the plan **currently executing** — associate them
> with that plan, not a differently-named one.

For each **spec** `docs/superpowers/specs/<date>-<name>.md`, upsert
`ext_key: "sp:spec:<name>"` into the `specs` doc view, `content:` the spec markdown.

For each **review diff** `.superpowers/sdd/review-*.diff`, upsert a card with
`column_key: "done"` (owned by the `reviews` view), `content:` the diff in a ```diff
fence.

Read each file and pass its **text as `content`** for the doc/list views to render.

## Keeping it live

Superpowers is subagent/worktree-driven and moves fast. Whenever a task report or
review lands, or you edit a plan/spec, re-`item_upsert` the affected card (same
`ext_key`) so the board tracks execution. Idempotent.

## Interactive setup

Propose the layout, then adjust with the user: whether Plans is a list or lanes,
how task done-ness is derived (report file vs progress.md), whether to surface
reviews. Re-`board_set_layout` until they're happy, then hydrate.

## Open it

<http://localhost:7777> — grouped under its project; live over SSE.
