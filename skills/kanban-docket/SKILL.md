---
name: kanban-docket
description: Use whenever you are working with a repo's docket backlog (a `.docket/` directory) — reviewing, grooming, triaging, or reporting on change manifests and ADRs, or picking what to work on next. Do not just read the manifests and answer; project them onto a live Workbench board so the human can see the backlog, then keep it current as you work. Triggers include "review/groom the backlog", "what should I work on next", "status of the docket work", "what's proposed/in flight/done", "which changes need a spec", as well as explicit "show the docket board" / "visualize docket".
---

# kanban-docket — project a docket backlog onto a kanban board

## What this is

[Docket](https://github.com/ethanhinson) tracks work as markdown **change
manifests** under `.docket/docs/changes/{active,archive}/*.md` (YAML-ish
frontmatter) plus **ADRs** under `docs/adrs/`.

**Workbench now projects a docket backlog deterministically, in the binary.** A
built-in adapter reads the change manifests and derives each card's placement from
its frontmatter — no manual upserting, no placement labels. The fastest path is:

```sh
workbench init --project <repo-abs-path>   # detect docket, create the board, set the
                                           # layout, hydrate every change, print the URL
```

Then run the server (`workbench --project <repo> --http :7777`) and it **watches the
change dir and re-syncs live** — edit a manifest (add a `branch:`, flip `status:`)
and the card moves on its own. This replaces the old "upsert each card by hand"
rhythm; placement is a pure function of the manifest, so the board can't drift.

Use the manual steps below only when you can't run `init` (e.g. a viz-less MCP
session) — and even then, prefer the `docket_sync` tool, which runs the same
deterministic adapter on demand.

## Prerequisites

The `kanban` MCP server is available (globally in Claude Code; see `/mcp`). It
serves the viz at <http://localhost:7777>. No `--repo-root` needed — content lives
on the cards you create.

## Step 1 — locate the docket docs

Docket lives on the repo's metadata branch, usually checked out at
`<repo>/.docket/docs`. Confirm `<repo>/.docket/docs/changes` exists. If the branch
isn't checked out, export it read-only first:

```sh
BR=$(grep -E '^metadata_branch:' "<repo>/.docket.yml" | awk '{print $2}'); BR=${BR:-docket}
git -C "<repo>" archive "$BR" docs | tar -x -C "$TMP"   # DOCS = $TMP/docs
```

## Step 2 — start the board (project-scoped)

```
board_start { name: "<repo>: docket backlog", project: "<repo-abs-path>", profile: "docket" }
```
Keep the returned `board_id`. The project groups this board under the repo in the UI.
**Pass `profile: "docket"`** — it binds the docket lifecycle columns, the change-type
lane dimension, and the docket gates. Omitting it falls back to the `sdd` profile,
which mislabels the board (e.g. a stray `spec:missing` badge on every card).

## Step 3 — design the docket layout, then set it

`workbench init` and `docket_sync` set this layout for you. Author it by hand only
when driving `board_set_layout` directly. **Placement is column-driven:** each view
*owns* the real docket profile columns, and a card's nav view is derived from its
`column_key` — there are no `view:`/`lane:` placement labels.

```
board_set_layout { board_id, layout: {
  nav: [
    { id: "backlog",  label: "Backlog",  view: "backlog" },
    { id: "inflight", label: "In Flight", view: "inflight" },
    { id: "done",     label: "Done",     view: "done" }
  ],
  views: {
    // each view OWNS real docket columns; lanes/board swimlane by owned column.
    "backlog":  { type: "lanes", group_by: "group",
                  columns: [{key:"backlog",label:"Needs Spec"},{key:"specifying",label:"Specifying"},{key:"specd",label:"Build-Ready"}] },
    "inflight": { type: "lanes", group_by: "group",
                  columns: [{key:"in_progress",label:"In Progress"},{key:"review",label:"In Review"}] },
    "done":     { type: "list",
                  columns: [{key:"done",label:"Done"},{key:"deferred",label:"Deferred"},{key:"killed",label:"Killed"}] }
  }
}}
```

## Step 4 — hydrate the cards (only if not using init / docket_sync)

Prefer `workbench init` or the `docket_sync` tool — they run the deterministic
adapter. If you must hydrate manually, for each change `item_upsert` a card keyed by
`docket:<id>` and set its **real `column_key`** (via `item_move` after upsert, or by
carrying it), which the renderer buckets by. The manifest → column mapping (first
match wins):

| Manifest state (first match wins) | `column_key` |
|---|---|
| `status: done` | `done` |
| `status: killed` | `killed` |
| `status: deferred` | `deferred` |
| `pr:` set (not terminal) | `review` |
| `branch:` set (not terminal, no pr) | `in_progress` |
| `status: in_progress` (no branch/pr) | `in_progress` |
| `trivial: true` (proposed, no branch/pr) | `specd` — the body *is* the spec |
| proposed, spec + plan | `specd` |
| proposed, spec, no plan | `specifying` |
| proposed, no spec | `backlog` |

Also: `type` (feat/fix/chore) → **`group:<type>`** chip; `blocked_by:` set →
`item_set_blocked`; `spec:` path → read the file, pass its markdown as `content`,
set `spec_ref`; `depends_on`/`related` → `item_link` to `docket:<n>`.

Read the spec/proposal/ADR file and pass its **text as `content`** — the doc view
renders that. Never rely on the server to read files.

## Keeping it live

The board tracks the session. **Whenever you edit a docket manifest, spec, or ADR
during your work, re-`item_upsert` that card** (same `ext_key`, refreshed `content`
and labels). Upsert is idempotent, so this only updates — never duplicates. A
full re-hydrate is just this loop over every manifest.

## Interactive setup

Board layout is a dialogue, not a one-shot. After proposing the layout:
- Show the user the nav tabs and each view's shape; ask what they want to see.
- Adjust: add/remove nav items, change a view between `lanes`/`board`/`list`/`doc`,
  rename lanes/columns, or change which docket states map where.
- Re-`board_set_layout` until they're happy. Then hydrate (Step 4).

## Open it

<http://localhost:7777> — pick the board in the sidebar (grouped under its project).
Live over SSE; your upserts appear immediately.
