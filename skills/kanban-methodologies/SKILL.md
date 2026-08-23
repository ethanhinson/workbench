---
name: kanban-methodologies
description: Use to choose how to set up a kanban-mcp board for a project's methodology — an index of the per-tool skills (docket, openspec, superpowers) and the shared agentic-layout model behind them. Trigger on "set up a kanban board for this repo", "which kanban skill", "put my backlog on a board".
---

# kanban-methodologies — pick the right board setup skill

kanban-mcp boards are **agentic**: the board's layout (nav tabs + views) and the
placement of every card are **data you author**, not hard-coded. A "methodology" is
just a **skill** — a prompt that reads a tool's files and projects them onto a
board with a tool-idiomatic shape. (OpenSpec ships its own integration as skills
too; skills are the emerging cross-tool standard.)

## Which skill?

Detect the project's methodology and use the matching skill:

| If the repo has… | Use skill | It builds |
|---|---|---|
| `.docket/docs/changes/` | **kanban-docket** | Backlog / In-Flight / ADRs / Done from docket manifests |
| `openspec/changes/` + `openspec/specs/` | **kanban-openspec** | Proposals / Tasks / Specs / Archive from OpenSpec |
| `docs/superpowers/{specs,plans}` | **kanban-superpowers** | Plans / In-Progress / Specs / Reviews |
| none of the above (ad-hoc session work) | **kanban-session** | a board you shape by hand as you work |

If more than one is present, ask the user which to project (or do several — each
becomes its own project-scoped board).

## The shared model (how every methodology skill works)

1. **`board_start`** a project-scoped board (`project:` = the repo dir) → `board_id`.
2. **`board_set_layout`** — declare the nav tabs + views. View types:
   - `list` — flat list of cards
   - `lanes` — swimlanes; **lanes are STATUS** (To Do/Doing/Done, or a pipeline)
   - `board` — vertical swimlanes only
   - `doc` — a markdown reader over cards' `content`
3. **`item_upsert`** each artifact (keyed by a stable `ext_key`), carrying:
   - `content` — the doc markdown you READ from the file (the server never reads
     files; you supply the content)
   - **`lane:<status>`** — which status lane the card sits in (the working axis)
   - **`group:<epic>`** — an epic/grouping (a change, plan, or type) shown as a
     color-coded chip on the card, so grouping is glanceable without an axis
   - `view:<v>` — which nav view the card appears in
   - `item_link` for dependencies
4. **Interactive setup** — the layout is a dialogue: propose it, adjust nav/views/
   lanes with the user, re-`board_set_layout` until they're happy.
5. **Keep it live** — the standing rhythm: *whenever you touch an artifact, upsert
   its card again* (same `ext_key`). Upsert is idempotent; the board stays as fresh
   as your work. A full re-hydrate is that loop over every artifact.

## Why prompts, not code

The server offers layout + upsert primitives; the *methodology* lives in the skill.
That keeps the server harness-agnostic and lets any tool (or a bespoke house
workflow) get a first-class board by writing one skill — no Go changes. To add a
new methodology, copy one of the three skills and remap its files → layout → labels.

## Open the board

<http://localhost:7777> — boards are grouped by project in the sidebar; live over SSE.
