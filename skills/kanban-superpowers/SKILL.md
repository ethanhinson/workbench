---
name: kanban-superpowers
description: Use when the user wants a live kanban board of a Superpowers SDD project — reading docs/superpowers/{specs,plans} and .superpowers execution state and projecting them onto a kanban-mcp board. Trigger on "show the superpowers board", "kanban of my plans", "visualize superpowers".
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
board** via kanban-mcp — you read the files, upsert the cards, declare the layout.

## Prerequisites

The `kanban` MCP server is available (see `/mcp`); viz at <http://localhost:7777>.

## Step 1 — locate the Superpowers artifacts

Confirm `<repo>/docs/superpowers/` exists (specs/ and/or plans/). Note any
`.superpowers/sdd/` execution state (task reports, review diffs, progress.md).

## Step 2 — start the board

```
board_start { name: "<repo>: superpowers", project: "<repo-abs-path>" }
```

## Step 3 — design the layout, then set it

Propose this (adjust interactively — see [Interactive setup](#interactive-setup)):

```
board_set_layout { board_id, layout: {
  nav: [
    { id: "plans",    label: "Plans",    view: "plans" },
    { id: "progress", label: "In Progress", view: "progress" },
    { id: "specs",    label: "Specs",    view: "specs" },
    { id: "reviews",  label: "Reviews",  view: "reviews" }
  ],
  views: {
    "plans":    { type: "list" },                         // each plan = a story
    "progress": { type: "lanes",
                  // lanes = the plan being executed; columns = task state
                  lanes:   [/* {key:<plan-slug>,label:<plan>} per active plan */],
                  columns: [{key:"todo",label:"To Do"},{key:"doing",label:"Doing"},{key:"done",label:"Done"}] },
    "specs":    { type: "doc" },
    "reviews":  { type: "list" }
  }
}}
```

**Ordering matters:** the `progress` view's lanes are **one per plan** — list the
plan files in Step 1 FIRST, build the `lanes` array from their slugs, then call
`board_set_layout` (a layout set before you know the plans has empty lanes).

## Step 4 — hydrate the cards (upsert by ext_key)

**`<name>` = the plan slug** — the filename with the `YYYY-MM-DD-` date prefix
stripped (e.g. `2026-07-12-m1-walkable-slice.md` → `m1-walkable-slice`). Use it
consistently for the plan's `ext_key`, its lane key, and its tasks' keys.

For each **plan** `docs/superpowers/plans/<date>-<name>.md`, `item_upsert`
`ext_key: "sp:plan:<name>"`: `view:plans`, `content:` the plan markdown,
`spec_ref:` the path. If it's actively executing, ALSO tag `view:progress`,
`lane:<name>`.

**Tasks — how Superpowers plans actually structure them:** a plan's tasks are
`### Task N: <title>` headings (sometimes sub-numbered `### Task N.M:`), each with
bite-sized `- [ ] Step …` items underneath. Parse the `### Task N` headings — NOT
the step checkboxes — one card per task. **Done-ness is authoritative in
`.superpowers/sdd/progress.md`**, a ledger of `Task N: complete …` lines; treat a
task as done if progress.md marks it complete (a `task-N-report.md` file is a
secondary hint). For each task, `item_upsert`
`ext_key: "sp:plan:<name>:task:<N>"`: `view:progress`, `lane:<name>`,
`column:done` if complete else `column:todo`; `item_link depends_on` the plan card;
pull `task-N-report.md` (if present) into `content`.

> Multiple plans can coexist under one `.superpowers/sdd/`. `progress.md` and the
> `task-N-*.md` files belong to the plan **currently executing** — associate them
> with that plan, not a differently-named one.

For each **spec** `docs/superpowers/specs/<date>-<name>.md`, upsert
`ext_key: "sp:spec:<name>"`, `view:specs`, `content:` the spec markdown.

For each **review diff** `.superpowers/sdd/review-*.diff`, upsert a `view:reviews`
card (`content:` the diff, in a ```diff fence).

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
