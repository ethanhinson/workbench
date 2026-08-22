---
name: kanban-docket
description: Use when the user wants a live kanban board of a repo's docket backlog — reading docket change manifests + ADRs and projecting them onto a kanban-mcp board with a docket-idiomatic layout. Trigger on "show the docket board", "kanban of the backlog", "visualize docket", "board for this repo's docket".
---

# kanban-docket — project a docket backlog onto a kanban board

## What this is

[Docket](https://github.com/ethanhinson) tracks work as markdown **change
manifests** under `.docket/docs/changes/{active,archive}/*.md` (YAML-ish
frontmatter) plus **ADRs** under `docs/adrs/`. This skill teaches you to read those
files and build a **docket-idiomatic kanban board** from them via kanban-mcp's MCP
tools — no importer, no server-side file reading. You read the files; you upsert
the cards; the board renders whatever layout you declare.

The board is a **live projection**: the hydration rhythm is *whenever you touch a
docket artifact, upsert its card* (see [Keeping it live](#keeping-it-live)).

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
board_start { name: "<repo>: docket backlog", project: "<repo-abs-path>" }
```
Keep the returned `board_id`. The project groups this board under the repo in the UI.

## Step 3 — design the docket layout, then set it

Propose this docket-idiomatic layout, but **treat it as a starting point — walk the
user through it and adjust before locking in** (see [Interactive setup](#interactive-setup)):

```
board_set_layout { board_id, layout: {
  nav: [
    { id: "backlog",  label: "Backlog",  view: "backlog" },
    { id: "inflight", label: "In Flight", view: "inflight" },
    { id: "adrs",     label: "ADRs",     view: "adrs" },
    { id: "done",     label: "Done",     view: "done" }
  ],
  views: {
    "backlog":  { type: "lanes",
                  lanes:   [{key:"feat",label:"Feature"},{key:"fix",label:"Fix"},{key:"chore",label:"Chore"},{key:"refactor",label:"Refactor"}],
                  columns: [{key:"needs_spec",label:"Needs Spec"},{key:"in_spec",label:"In Spec"},{key:"build_ready",label:"Build-Ready"}] },
    "inflight": { type: "lanes",
                  lanes:   [{key:"spec",label:"Spec"},{key:"build",label:"Build"},{key:"review",label:"Review"}],
                  columns: [{key:"doing",label:"Doing"},{key:"blocked",label:"Blocked"}] },
    "adrs":     { type: "doc" },
    "done":     { type: "list" }
  }
}}
```

## Step 4 — hydrate the cards (upsert by ext_key)

For each change manifest, `item_upsert` a card keyed by `docket:<id>`. Map the
frontmatter to **placement labels** so it lands in the right view/lane/column:

| Manifest field | Card field / label |
|---|---|
| `id` + `title` | `title: "#<id> <title>"`, `ext_key: "docket:<id>"` |
| `type` (feat/fix/chore/refactor) | `lane:<type>` (in Backlog) |
| `status: done\|killed\|deferred` | `view:done` |
| proposed, no spec | `view:backlog`, `column:needs_spec` |
| proposed, spec but no plan | `view:backlog`, `column:in_spec` |
| proposed, spec+plan | `view:backlog`, `column:build_ready` |
| `branch:` set (in progress) | `view:inflight`, `lane:build`, `column:doing` |
| `pr:` set (review) | `view:inflight`, `lane:review`, `column:doing` |
| `blocked_by:` set | add `column:blocked`; set `spec_ref`/note |
| `spec:` path | read that file; pass its markdown as `content`; set `spec_ref` |
| `depends_on` / `discovered_from` / `related` | `item_link` from this card to the referenced `docket:<n>` |

Example upsert for one change:
```
item_upsert {
  board_id, ext_key: "docket:64", title: "#64 bash egress control",
  content: "<the full markdown of the spec/proposal you read>",
  spec_ref: "docs/superpowers/specs/....md",
  labels: ["view:inflight", "lane:spec", "column:doing", "area:feat"]
}
```

For each **ADR** under `docs/adrs/`, upsert `ext_key: "adr:<id>"`, `view:adrs`,
`content:` the ADR markdown, and `item_link` it to the `docket:<change>` it came
from.

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
