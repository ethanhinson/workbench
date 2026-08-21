---
name: kanban-docket-sync
description: Use when you want to see a docket backlog as a kanban board — importing a repo's docket change manifests into kanban-mcp and opening the visual board. Works with any harness because docket stores work as markdown on the metadata branch; this reads that markdown directly. Trigger on "show the docket board", "kanban view of the backlog", "visualize docket".
---

# kanban-docket-sync — docket backlog → kanban pane of glass

## Overview

Docket tracks work as markdown **change manifests** (`docs/changes/{active,archive}/*.md`)
with YAML-ish frontmatter, committed to the repo's **metadata branch** (usually
`docket`). That markdown-on-a-branch shape is harness-agnostic: it does not matter
which agent or tool drives docket — the files are the interface.

`kanban-mcp` reads those files and maps each change onto a kanban board (columns =
docket status, swim lane = change type), then serves a browsable board + a JSON
Snapshot API. This skill wires the two together for any repo.

## Mapping (what a renderer will show)

| Docket manifest field | Kanban board |
|---|---|
| change `id` + `title` | a card titled `#<id> <title>`, keyed `docket:<id>` (idempotent) |
| `status` proposed / deferred / done / killed | column (proposed refines by artifacts, below) |
| proposed + `branch:` set | **In Progress** |
| proposed + `pr:` set | **In Review** |
| proposed + `spec:`+`plan:` present | **Build-Ready** |
| proposed, no spec/plan | **Backlog** (labelled `needs-brainstorm`) |
| `priority` high / medium / low | `priority:` p0 / p1 / p3 |
| `type` feat / fix / chore / … | swim **lane** (fix ⇒ card kind `bug`) |
| `spec:` / `results:` presence | `spec_status` missing / draft / approved |
| `blocked_by:` set | card flagged **blocked** |
| single `discovered_from` / `depends_on` parent (also imported) | nested under that parent |

Re-running the sync **updates in place** (keyed by `docket:<id>`), never duplicates.

## Steps

### 0. Locate the docket docs directory

The manifests live on the metadata branch. Resolve `DOCS_DIR` in this order:

1. **Checked-out worktree** (most common): `<repo>/.docket/docs` exists on disk.
   ```sh
   test -d "<repo>/.docket/docs" && DOCS_DIR="<repo>/.docket/docs"
   ```
2. **On the metadata branch only** (no worktree checkout): export it to a temp dir.
   Read the branch name from `.docket.yml` (`metadata_branch:`, default `docket`):
   ```sh
   BR=$(grep -E '^metadata_branch:' "<repo>/.docket.yml" | awk '{print $2}'); BR=${BR:-docket}
   TMP=$(mktemp -d)
   git -C "<repo>" archive "$BR" docs | tar -x -C "$TMP"
   DOCS_DIR="$TMP/docs"
   ```
   `git archive` is read-only and needs no worktree/branch switch — safe with any harness mid-work.

Confirm `DOCS_DIR/changes` exists before continuing; if not, this repo has no docket backlog.

### 1. Sync into a plan

Two equivalent paths — pick by whether an MCP server is already connected:

- **CLI (one-shot, no MCP):**
  ```sh
  kanban-mcp --db /tmp/<repo>-board.db --plan "<repo> Backlog" \
    --profile docket --docket-sync "$DOCS_DIR"
  ```
- **MCP tool (from an agent):** call `docket_sync` with `{ "docs_dir": "<DOCS_DIR>" }`.
  The plan should have been created with `--profile docket`.

Both are idempotent — safe to re-run whenever the backlog changes.

### 2. Open the board

```sh
kanban-mcp --db /tmp/<repo>-board.db --plan "<repo> Backlog" --viz-only --http :7777
```
Then open <http://localhost:7777>. The board auto-refreshes; `GET /api/board`
returns the same Snapshot for any custom renderer. Re-run step 1 to pull new
changes; the open board picks them up on its next refresh.

## Notes for any harness

- Nothing here is Claude-specific. The contract is: docket frontmatter on a branch
  in, `docket_sync` (or `--docket-sync`) applies the mapping, the Snapshot / SPA
  render it. A different agent runtime calls the same MCP tool or CLI flag.
- The board is **read-only** over the docket data — kanban-mcp never writes back to
  docket. Docket remains the source of truth; the board is the pane of glass.
- To keep it live, run step 1 on a loop (e.g. every few minutes) against the same db.
