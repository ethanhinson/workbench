---
name: kanban-openspec
description: Use when the user wants a live kanban board of an OpenSpec project — reading openspec/changes and openspec/specs and projecting them onto a kanban-mcp board with an OpenSpec-idiomatic layout. Trigger on "show the openspec board", "kanban of my openspec changes", "visualize openspec".
---

# kanban-openspec — project an OpenSpec workflow onto a kanban board

## What this is

[OpenSpec](https://github.com/Fission-AI/OpenSpec) is spec-driven development for AI
assistants. It separates **current truth** (`openspec/specs/<capability>/spec.md`)
from **proposed changes** (`openspec/changes/<name>/` folders, each with
`proposal.md`, `tasks.md`, `design.md`, sometimes `specs/`). Completed changes move
to `openspec/changes/archive/`.

This skill teaches you to read those files and build an **OpenSpec-idiomatic kanban
board** via kanban-mcp's MCP tools — you read the files, upsert the cards, and
declare the layout. No server-side file reading.

## Prerequisites

The `kanban` MCP server is available (see `/mcp`); viz at <http://localhost:7777>.

## Step 1 — locate the OpenSpec tree

Confirm `<repo>/openspec/changes` and `<repo>/openspec/specs` exist. List the
change folders and the spec capabilities.

## Step 2 — start the board

```
board_start { name: "<repo>: openspec", project: "<repo-abs-path>" }
```

## Step 3 — design the OpenSpec layout, then set it

Propose this (adjust interactively — see [Interactive setup](#interactive-setup)):

```
board_set_layout { board_id, layout: {
  nav: [
    { id: "proposals", label: "Proposals", view: "proposals" },
    { id: "tasks",     label: "Tasks",     view: "tasks" },
    { id: "specs",     label: "Specs",     view: "specs" },
    { id: "archive",   label: "Archive",   view: "archive" }
  ],
  views: {
    "proposals": { type: "lanes",
                   lanes:   [{key:"draft",label:"Draft"},{key:"approved",label:"Approved"}],
                   columns: [{key:"has_design",label:"Has Design"},{key:"no_design",label:"No Design"}] },
    "tasks":     { type: "lanes",
                   // one lane per active change; columns track tasks.md checkbox progress
                   lanes:   [/* {key:<change>,label:<change>} per active change */],
                   columns: [{key:"todo",label:"To Do"},{key:"doing",label:"Doing"},{key:"done",label:"Done"}] },
    "specs":     { type: "doc" },
    "archive":   { type: "list" }
  }
}}
```

The `tasks` view's lanes are **one per active change** — build that list from the
change folders you found, then set the layout.

## Step 4 — hydrate the cards (upsert by ext_key)

For each **change** `openspec/changes/<name>/`, `item_upsert` a proposal card keyed
`openspec:<name>`:

| OpenSpec artifact | Card field / label |
|---|---|
| change `<name>` | `title: <name>`, `ext_key: "openspec:<name>"` |
| `proposal.md` | pass its markdown as `content`; `view:proposals` |
| `design.md` present? | `lane:draft`/`approved` heuristic; `column:has_design`/`no_design` |
| in `changes/archive/` | `view:archive` instead |

For each **task** in a change's `tasks.md` (numbered `- [ ] N.M …` checkboxes),
`item_upsert` a task card keyed `openspec:<name>:task:<N.M>`:
- `view:tasks`, `lane:<change>` (the change's key), `column:done` if `[x]` else `column:todo`.
- `item_link` each task card `depends_on` its proposal card.

For each **spec** `openspec/specs/<cap>/spec.md`, upsert `ext_key: "openspec:spec:<cap>"`,
`view:specs`, `content:` the spec markdown, `spec_ref:` the path.

Read each file and pass its **text as `content`** so the doc view renders it.

## Keeping it live

Whenever you edit a proposal, check off a task, or update a spec during your work,
re-`item_upsert` that card (same `ext_key`). Checking a `tasks.md` box → re-upsert
that task card with `column:done`. Idempotent; only updates.

## Interactive setup

Propose the layout, then adjust with the user: which nav tabs, whether Tasks is a
lanes grid or a per-change list, how proposal draft/approved is decided, etc.
Re-`board_set_layout` until they're happy, then hydrate.

## Open it

<http://localhost:7777> — the board is grouped under its project; live over SSE.
