package adapter

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/ethanhinson/workbench/internal/board"
	"github.com/ethanhinson/workbench/internal/store"
)

// docketAdapter projects a docket backlog (change manifests on the docket metadata
// branch) onto a board. Placement is derived deterministically from each change's
// frontmatter by PlaceChange — no labels, no activity feed.
type docketAdapter struct {
	// dir, when non-empty, is an explicit changes dir (from --docket-dir / env) that
	// overrides convention-based resolution.
	dir string
}

// NewDocketAdapter returns the default docket adapter (convention-based dir
// resolution). Set KANBAN_DOCKET_DIR to override the changes dir.
func NewDocketAdapter() Adapter {
	return &docketAdapter{dir: os.Getenv("KANBAN_DOCKET_DIR")}
}

func (a *docketAdapter) Name() string { return "docket" }

// changesDirDefault is the conventional path (relative to a repo) that holds the
// change manifests; docket's metadata branch is checked out under .docket/.
const changesDirDefault = "docs/changes"

// ChangeDir resolves the change directory deterministically:
//  1. an explicit override (a.dir / --docket-dir), used as-is;
//  2. the docket-mode worktree: <repo>/.docket/docs/changes (the common case);
//  3. main-mode (single-branch): <repo>/docs/changes.
//
// It returns the first that contains an active/ subdir, and whether one resolved.
func (a *docketAdapter) ChangeDir(repoDir string) (string, bool) {
	candidates := []string{}
	if a.dir != "" {
		candidates = append(candidates, a.dir)
	}
	candidates = append(candidates,
		filepath.Join(repoDir, ".docket", changesDirDefault),
		filepath.Join(repoDir, changesDirDefault),
	)
	for _, c := range candidates {
		if isDir(filepath.Join(c, "active")) {
			return c, true
		}
	}
	return "", false
}

// Detect reports whether a docket footprint exists at repoDir.
func (a *docketAdapter) Detect(repoDir string) bool {
	_, ok := a.ChangeDir(repoDir)
	return ok
}

// Sync reads every change manifest (active + archive), maps each to a board item,
// upserts it keyed by docket:<id>, reconciles deletes, and applies depends_on /
// related links. A malformed manifest is logged and skipped, never fatal.
func (a *docketAdapter) Sync(ctx context.Context, st *store.Store, planID, repoDir string) error {
	dir, ok := a.ChangeDir(repoDir)
	if !ok {
		return fmt.Errorf("no docket footprint under %q", repoDir)
	}
	files, err := changeFiles(dir)
	if err != nil {
		return err
	}

	seen := map[string]bool{}
	type link struct{ fromID, toDocket, kind string }
	var links []link

	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			log.Printf("docket: read %s: %v", f, err)
			continue
		}
		ch, err := ParseChange(data)
		if err != nil {
			log.Printf("docket: parse %s: %v", f, err)
			continue
		}
		if ch.ID == 0 {
			log.Printf("docket: skip %s: no id", f)
			continue
		}
		it := ChangeToItem(planID, ch)
		if _, err := st.UpsertByExtKey(ctx, "docket", it); err != nil {
			log.Printf("docket: upsert %s: %v", it.ExtKey, err)
			continue
		}
		seen[it.ExtKey] = true
		for _, dep := range ch.DependsOn {
			links = append(links, link{it.ExtKey, docketExtKey(dep), "depends_on"})
		}
		for _, rel := range ch.Related {
			links = append(links, link{it.ExtKey, docketExtKey(rel), "related"})
		}
	}

	// Reconcile deletes: any docket:* item not seen this pass was removed from disk.
	if err := a.reconcileDeletes(ctx, st, planID, seen); err != nil {
		return err
	}

	// Apply links now that every item exists (resolve ext_key → id).
	for _, l := range links {
		fromID, ok1 := st.ItemIDByExtKey(ctx, planID, l.fromID)
		toID, ok2 := st.ItemIDByExtKey(ctx, planID, l.toDocket)
		if !ok1 || !ok2 {
			continue
		}
		if err := st.AddLink(ctx, planID, fromID, toID, l.kind); err != nil {
			log.Printf("docket: link %s->%s: %v", l.fromID, l.toDocket, err)
		}
	}
	return nil
}

// reconcileDeletes removes docket items whose manifest is gone from disk.
func (a *docketAdapter) reconcileDeletes(ctx context.Context, st *store.Store, planID string, seen map[string]bool) error {
	items, err := st.ListItems(ctx, planID, store.Filter{})
	if err != nil {
		return err
	}
	for _, it := range items {
		if !strings.HasPrefix(it.ExtKey, "docket:") {
			continue
		}
		if seen[it.ExtKey] {
			continue
		}
		if err := st.DeleteItem(ctx, planID, it.ID); err != nil {
			log.Printf("docket: delete %s: %v", it.ExtKey, err)
		}
	}
	return nil
}

// ChangeToItem builds the board item for a change. It sets the real column_key
// (from PlaceChange) so the renderer places it; the change type is a group chip.
func ChangeToItem(planID string, ch Change) *board.Item {
	p := PlaceChange(ch)
	return &board.Item{
		PlanID:    planID,
		Kind:      board.KindTask,
		Title:     fmt.Sprintf("#%d %s", ch.ID, ch.Title),
		Content:   ch.Body,
		ColumnKey: p.ColumnKey,
		LaneKey:   p.Group, // docket lane dimension is "type"; kept for a future type-grid
		Priority:  p.Priority,
		Blocked:   p.Blocked,
		ExtKey:    docketExtKey(ch.ID),
		Labels:    []board.Label{{NS: "group", Value: p.Group}},
	}
}

func docketExtKey(id int) string { return fmt.Sprintf("docket:%d", id) }

// changeFiles returns the .md manifests under active/ and archive/, skipping the
// generated BOARD.md.
func changeFiles(dir string) ([]string, error) {
	var out []string
	for _, sub := range []string{"active", "archive"} {
		entries, err := os.ReadDir(filepath.Join(dir, sub))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			if e.Name() == "BOARD.md" {
				continue
			}
			out = append(out, filepath.Join(dir, sub, e.Name()))
		}
	}
	return out, nil
}

func isDir(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

// DocketLayout is the canonical docket board layout: nav tabs Backlog / In Flight /
// ADRs / Done, with each work view owning the real docket columns so an item's view
// derives from its column_key. Backlog and In Flight are lanes views (swimlanes =
// owned columns); Done is a list. It is the single source of truth for both init
// and the watcher.
func DocketLayout() board.Layout {
	return board.Layout{
		Nav: []board.NavItem{
			{ID: "backlog", Label: "Backlog", View: "backlog"},
			{ID: "inflight", Label: "In Flight", View: "inflight"},
			{ID: "done", Label: "Done", View: "done"},
		},
		Views: map[string]board.LayoutView{
			"backlog": {
				Type: board.ViewLanes,
				Columns: []board.LayoutAxis{
					{Key: ColBacklog, Label: "Needs Spec"},
					{Key: ColSpecifying, Label: "Specifying"},
					{Key: ColSpecd, Label: "Build-Ready"},
				},
			},
			"inflight": {
				Type: board.ViewLanes,
				Columns: []board.LayoutAxis{
					{Key: ColInProgress, Label: "In Progress"},
					{Key: ColReview, Label: "In Review"},
				},
			},
			"done": {
				Type: board.ViewList,
				Columns: []board.LayoutAxis{
					{Key: ColDone, Label: "Done"},
					{Key: ColDeferred, Label: "Deferred"},
					{Key: ColKilled, Label: "Killed"},
				},
			},
		},
	}
}
