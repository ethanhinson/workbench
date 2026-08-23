# Agentic layout — the board layout is data the agent authors

**Status:** proposal (for review before implementation)
**Supersedes:** the hard-coded 3-view model (`internal/board/views.go`,
`PlaceInView`) and server-side spec-path resolution (`--repo-root`, viz `readRef`)

## The idea

Today a board's shape is fixed in Go: three tabs (Backlog / In-Flight / Done),
each with hard-coded lanes and columns, and `PlaceInView` decides where each item
lands from its `column_key`/`spec_status`. Every board looks the same.

We want the **layout to be data the agent authors per board**. The viz becomes a
*generic renderer* of whatever layout a board declares. A methodology (OpenSpec,
Docket, Superpowers) is a **skill** — a prompt that reads the tool's files, designs
a tool-idiomatic layout, fills it, and (interactively) tunes it with the user.

Nothing about the source tool is special-cased in Go. Docket stops being a
built-in importer; it becomes a skill like the others. The server offers a small
set of layout primitives; the *skill* supplies the methodology.

## Regions

The viz shell exposes a fixed set of **named regions** — structural slots that
always exist. The agent fills them; it does not invent new regions.

- **`nav`** — the tab/menu strip. The agent decides how many nav items, their
  labels, and which **view** each opens.
- **`swimlanes`** — the main board area for a lane/grid view.
- (room to add more later: `header`, `detail`, `filters` — start with nav + the
  view body.)

## The layout schema

A board carries one `layout` object (JSON). Shape:

```jsonc
{
  "nav": [                          // the nav strip, left→right
    { "id": "inflight", "label": "In Flight", "view": "inflight" },
    { "id": "specs",    "label": "Specs",     "view": "specs" },
    { "id": "done",     "label": "Done",      "view": "done" }
  ],
  "views": {
    "inflight": {
      "type": "lanes",              // list | lanes | board | doc
      "lanes":   [ { "key": "build",  "label": "Build" },
                   { "key": "review", "label": "Review" } ],
      "columns": [ { "key": "todo",  "label": "To Do" },
                   { "key": "doing", "label": "Doing" } ],
      "include": { "view": "inflight" },   // which items appear (see Placement)
      "sort": "priority"
    },
    "specs": {
      "type": "doc",                // a markdown reader over items' `content` field
      "include": { "view": "specs" }
    },
    "done": {
      "type": "list",
      "include": { "view": "done" },
      "sort": "updated_desc"
    }
  }
}
```

### View types (what the renderer knows how to draw)

| type    | shape | today's analog |
|---------|-------|----------------|
| `list`  | flat scrollable list of item rows | the Done tab |
| `lanes` | columns × swimlanes grid of cards | the In-Flight tab |
| `board` | vertical swimlanes only, cards stacked (no column axis) | classic kanban |
| `doc`   | rendered-markdown reader over the items' `content` field | (new) — Specs/ADRs |

A `lanes` view needs `lanes` + `columns`; a `board` view needs `lanes`; `list` and
`doc` need neither.

## Placement — explicit tags, no Go logic

The renderer does **no** placement computation. Each item declares where it lives
via labels the skill applies:

- **`view:<view-id>`** — which view(s) the item appears in (an item may carry more
  than one, e.g. a build-ready change shows in both `inflight` and `specs`).
- **`lane:<lane-key>`** — its swimlane within a `lanes`/`board` view.
- **`column:<column-key>`** — its column within a `lanes` view.

A view's `include` selects items by the tags it cares about (default:
`view:<the view's id>`). The renderer buckets matching items by their `lane:` /
`column:` tags into the view's declared lanes/columns. An item with no `lane:`
tag falls into a synthesized "unassigned" lane so nothing is lost.

This means the same board db, re-tagged, re-renders under any layout — the layout
and the tags are the whole contract.

### Label taxonomy change

Placement needs three new label namespaces: **`view`, `lane`, `column`**. Today
`ValidateLabel` enum-restricts namespaces to `type, priority, spec, stage, agent,
area`. We add `view`/`lane`/`column` as **open** namespaces (any slug value, like
`agent`/`area`), since their valid values are defined by the board's own layout,
not a global enum. The renderer ignores a `lane:`/`column:` value the layout
doesn't declare (falls back to unassigned) rather than erroring — layouts evolve.

## Content, not paths — the server never reads the filesystem

Today an item stores a `spec_ref` *path* and the viz resolves it to file *content*
at render time under `--repo-root`. That coupling is a footgun: the server has to
know a filesystem root, and it breaks whenever the ref is relative to something
else (e.g. docket paths relative to `.docket/` while the root points at the repo).

We remove it entirely. The methodology skill is **already reading these files** to
build the board — so it puts the **content into the item**, not a path to resolve
later:

- Items gain a **`content`** field (markdown): the rendered spec / ADR / notes body
  the `doc` view displays. `body` stays for short free-text notes; `content` is the
  document. The skill fills `content` when it creates the card.
- `spec_ref` survives as a **plain optional string** — a citation/link shown for
  provenance (e.g. `openspec/specs/foo/spec.md`), **never resolved server-side**.
- **Deleted:** the `--repo-root` flag, viz `readRef()`, the `readFile` plumbing
  threaded through `store.ItemDetail`, and `SpecContent`/`PlanContent` on
  `ItemDetail`. The server touches no files for content; everything the renderer
  needs is in the snapshot.

Net effect: the `doc` view (and the detail drawer) render `item.content` directly —
no path resolution, no root, no footgun.

## Hydration — the board is a live projection of the session

Where does `content` come from, and how does it stay current? **From the agent, as
a byproduct of the work it's already doing.** There is no separate sync engine, no
watcher, no cron.

- **Trigger — as the agent works.** When the agent creates or edits a methodology
  artifact during a session (writes `openspec/changes/auth/proposal.md`, updates a
  spec, checks off a task), it *also* upserts that artifact's card in the same beat.
  The board tracks the session live (the generalized `kanban-session` path). It is
  as fresh as the work.
- **Identity — `ext_key` upsert (already built).** Each card is keyed by a stable
  source id the skill computes: `openspec:<change>`, `docket:<id>`,
  `superpowers:<plan-slug>`. Re-touching the same artifact **updates** its card;
  a new artifact **creates** one. `store.UpsertByExtKey` already does exactly this
  (it's how the docket importer stayed idempotent) — the skill just supplies the
  key + content instead of the Go importer.
- **Engine — the agent inline (pure skill).** The agent reads the file and calls
  the MCP tool. No server-side code, no headless job. The cost is agent tokens,
  paid only when the agent is already in that file.

So the skill's core instruction is a **rhythm**: *"whenever you touch a methodology
artifact, upsert its card (ext_key + content + placement tags)."* Hydration and
freshness are the same act. A one-shot "hydrate the whole board now" is just that
rhythm applied in a loop over every existing artifact — the same upserts, batched.

## No default layout

A freshly `board_start`-ed board has **no layout and renders nothing** until one is
set. This is deliberate: the layout is the methodology's job. A bare board with no
skill is an empty canvas, not a pre-opinionated 3-tab board. (Trade-off: opening
the viz on a layout-less board shows an empty state with "no layout set — run a
methodology skill or set_layout". Acceptable; keeps the model pure.)

## MCP surface

- **`board_set_layout`** `{ board_id, layout }` — validate and store the layout JSON
  on the board. Idempotent (replaces). Validation: every `nav[].view` must key into
  `views`; every view `type` must be known; `lanes`/`columns` required for the types
  that need them. Returns a summary.
- **`board_get_layout`** `{ board_id }` — return the current layout (so a skill can
  read/tweak rather than reauthor).
- **`item_upsert`** `{ board_id, ext_key, title, content?, labels?, spec_ref?,
  kind?, … }` — the hydration primitive: create-or-update a card keyed by
  `(board_id, ext_key)`. Carries `content` (the doc markdown) and placement labels
  (`view:`/`lane:`/`column:`). Wraps the existing `store.UpsertByExtKey`, now
  exposed to agents instead of only the (removed) docket Go importer. This is what
  the methodology skills call as they work.
- `item_create`/`item_label` still exist for ad-hoc, non-source-keyed cards (a
  live session board the agent invents). Both already accept `ns:value` labels, so
  tagging uses the calls a skill already knows.

The item's `content` is set via `item_upsert` (or a small `item_set_content
{ item_id, content }` for edits to an existing card). No filesystem access anywhere
in this path.

## Storage

- `plan` gains a `layout TEXT NOT NULL DEFAULT ''` column (empty = no layout).
  Small, additive; no migration concerns (nothing deployed).
- `item` gains a `content TEXT NOT NULL DEFAULT ''` column (the rendered doc the
  agent supplies for the `doc` view).
- No new tables — placement is labels, which already exist.

## Snapshot (v3 → v4)

- **Remove:** `Views []ViewDef` and each item's computed `Views` placement map
  (the old `PlaceInView` output), replaced by layout + tags. Also remove
  `ItemDetail.SpecContent`/`PlanContent` and the `readFile` parameter — content is
  now on the item.
- **Add:** `Layout` (the board's layout object); `content` on each item.
- `internal/board/views.go` + `PlaceInView` are **deleted**. So is the viz
  `readRef` / `--repo-root` file-resolution path.

The Snapshot stays the single renderer-agnostic contract: `{ plan, layout, items
(+labels +content), links, stats }`. Any renderer reads the layout, buckets items
by tag, and renders `content` for doc views — with no filesystem access.

## Renderer (the SPA becomes generic)

The bundled SPA stops hard-coding three tabs. Instead:

1. Read `snapshot.layout.nav` → render the nav strip.
2. On selecting a nav item → render its `view` by `type`:
   - `list` → the existing Done-style list, filtered by `include`.
   - `lanes` → the existing grid, using the view's declared lanes/columns, bucketing
     items by their `lane:`/`column:` tags.
   - `board` → vertical swimlanes by `lane:` tag.
   - `doc` → the existing markdown/mermaid reader (already built for the detail
     drawer), rendering each view item's `content` field.
3. Empty state when `layout` is absent.

All four view types reuse rendering code that already exists (list, grid, vertical
lanes, markdown reader) — this is mostly *routing by layout* rather than new UI.

## The skill contract (each methodology)

Each `skills/kanban-<tool>/SKILL.md` teaches the agent to:

1. **Locate** the tool's artifacts (verified real layouts):
   - OpenSpec: `openspec/specs/<cap>/spec.md`, `openspec/changes/<name>/{proposal,tasks,design}.md`, `changes/archive/`.
   - Superpowers: `docs/superpowers/specs/YYYY-MM-DD-*.md`, `docs/superpowers/plans/YYYY-MM-DD-*.md`, `.superpowers/` state.
   - Docket: `.docket/docs/changes/{active,archive}/NNNN-*.md`, `docs/adrs/`, `docs/specs/`.
2. **`board_start`** a project-scoped board (project = the repo dir).
3. **Design a tool-idiomatic layout** and `board_set_layout` it. Examples:
   - OpenSpec → nav: `Proposals`(lanes: draft/approved), `Tasks`(lanes by change, columns from tasks.md checkbox state), `Specs`(doc), `Archive`(list).
   - Superpowers → nav: `Plans`(list), `In Progress`(lanes by plan phase), `Specs`(doc), `Reviews`(list of review diffs).
   - Docket → nav: `Backlog`(lanes by type), `In Flight`(lanes: spec/build/review), `ADRs`(doc), `Done`(list).
4. **Hydrate via `item_upsert`** — for each artifact, `item_upsert` a card keyed by
   `ext_key` (`openspec:<change>` etc.), carrying `content` = the file's markdown and
   the `view:`/`lane:`/`column:` placement labels; `item_link` for deps. The skill
   reads the file and passes its text — the server never opens it. Upsert makes this
   idempotent, so re-running only updates.
5. **Keep it live** — the skill's standing rhythm: *whenever the agent touches a
   methodology artifact during the session, `item_upsert` that card again.* The
   board stays as fresh as the work; no separate sync step.
6. **Interrogate the user interactively** — walk them through the proposed layout,
   adjust nav items / lanes / which views are lists vs grids, and re-`board_set_layout`
   until they're happy. Board setup is a dialogue, not a one-shot import.

## Implementation slices

1. **Layout core** — `plan.layout` column; `Layout` types in `internal/board`;
   `board_set_layout` / `board_get_layout` tools + validation; label namespaces
   `view/lane/column`. Tests.
2. **Content + hydration + de-footgun** — `item.content` column; expose
   `item_upsert` (wraps `UpsertByExtKey`, carries content + labels) and
   `item_set_content`; delete `--repo-root`, viz `readRef`, the `readFile` plumbing
   in `ItemDetail`, and `SpecContent`/`PlanContent`. Tests.
3. **Snapshot v4** — add `Layout` + item `content`, remove `Views`/`PlaceInView`;
   delete `views.go`. Update store `Snapshot`. Tests.
4. **Generic renderer** — SPA reads layout, routes the 4 view types (doc renders
   `content`). Empty state. Drop the SPA's spec-fetch-under-root path.
5. **Skills** — `kanban-openspec`, `kanban-superpowers`, refresh `kanban-docket`
   (drop the Go-importer framing) + a `kanban-methodologies` index. Each includes
   the interactive setup flow.
6. **Verify** — drive each methodology end-to-end over the wire against a real repo
   (fuse=docket, code-indexer=openspec, a `.superpowers` repo), screenshot.

## Open questions / risks

- **Content size:** the agent inlines doc markdown into `item.content`, so large
  specs live in the db and ship in every snapshot/SSE frame. Fine for typical spec
  sizes; if it ever matters, the renderer can lazy-load `content` via the item
  detail endpoint instead of the board snapshot. (Start inline; optimize only if
  needed.)
- **Multi-view items:** an item in two views needs two `view:` tags; fine, but the
  skill must be disciplined about tagging. The renderer tolerates missing tags
  (unassigned bucket) so partial tagging degrades gracefully.
- **Backwards data:** existing boards (the fuse docket mirror) have no layout →
  they'd render empty until a skill sets one. Acceptable given no-default; we can
  ship the docket skill first and re-layout that board.
