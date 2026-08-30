package adapter

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ethanhinson/workbench/internal/store"
)

// writeChange writes a manifest file under <dir>/<sub>/<name>.
func writeChange(t *testing.T, dir, sub, name, body string) {
	t.Helper()
	d := filepath.Join(dir, sub)
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// setup returns a store, a docket board, the change dir, and the adapter pointed at it.
func setup(t *testing.T) (*store.Store, string, string, Adapter) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "b.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	plan, err := st.CreatePlan(context.Background(), "docket board", "/proj", "", "docket")
	if err != nil {
		t.Fatal(err)
	}
	changes := filepath.Join(t.TempDir(), "docs", "changes")
	a := &docketAdapter{dir: changes}
	return st, plan.ID, changes, a
}

func itemByExt(t *testing.T, st *store.Store, planID, ext string) (col string, found bool) {
	t.Helper()
	items, err := st.ListItems(context.Background(), planID, store.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range items {
		if it.ExtKey == ext {
			return it.ColumnKey, true
		}
	}
	return "", false
}

func TestSyncPlacesChanges(t *testing.T) {
	st, planID, dir, a := setup(t)
	ctx := context.Background()

	writeChange(t, dir, "active", "0001-a.md", "---\nid: 1\ntitle: A\nstatus: proposed\ntype: feat\n---\nno spec")
	writeChange(t, dir, "active", "0002-b.md", "---\nid: 2\ntitle: B\nstatus: proposed\ntype: fix\nbranch: feat/b\n---\nbuilding")
	writeChange(t, dir, "archive", "2026-01-01-0003-c.md", "---\nid: 3\ntitle: C\nstatus: done\ntype: chore\n---\ndone")

	if err := a.Sync(ctx, st, planID, "/unused"); err != nil {
		t.Fatal(err)
	}

	for ext, wantCol := range map[string]string{
		"docket:1": ColBacklog,
		"docket:2": ColInProgress,
		"docket:3": ColDone,
	} {
		col, ok := itemByExt(t, st, planID, ext)
		if !ok {
			t.Fatalf("%s not found", ext)
		}
		if col != wantCol {
			t.Errorf("%s column = %q, want %q", ext, col, wantCol)
		}
	}
}

func TestSyncIdempotent(t *testing.T) {
	st, planID, dir, a := setup(t)
	ctx := context.Background()
	writeChange(t, dir, "active", "0001-a.md", "---\nid: 1\ntitle: A\nstatus: proposed\n---\nx")

	_ = a.Sync(ctx, st, planID, "/x")
	_ = a.Sync(ctx, st, planID, "/x")

	items, _ := st.ListItems(ctx, planID, store.Filter{})
	if len(items) != 1 {
		t.Fatalf("expected 1 item after two syncs, got %d", len(items))
	}
}

func TestSyncUpdateMovesInPlace(t *testing.T) {
	st, planID, dir, a := setup(t)
	ctx := context.Background()
	path := "0001-a.md"
	writeChange(t, dir, "active", path, "---\nid: 1\ntitle: A\nstatus: proposed\n---\nx")
	_ = a.Sync(ctx, st, planID, "/x")
	if col, _ := itemByExt(t, st, planID, "docket:1"); col != ColBacklog {
		t.Fatalf("initial column = %q, want backlog", col)
	}

	// Add a spec → should move to specifying, same item id.
	writeChange(t, dir, "active", path, "---\nid: 1\ntitle: A\nstatus: proposed\nspec: s.md\n---\nx")
	_ = a.Sync(ctx, st, planID, "/x")

	items, _ := st.ListItems(ctx, planID, store.Filter{})
	if len(items) != 1 {
		t.Fatalf("expected still 1 item, got %d", len(items))
	}
	if col, _ := itemByExt(t, st, planID, "docket:1"); col != ColSpecifying {
		t.Fatalf("after spec, column = %q, want specifying", col)
	}
}

func TestSyncDeleteReconciles(t *testing.T) {
	st, planID, dir, a := setup(t)
	ctx := context.Background()
	writeChange(t, dir, "active", "0001-a.md", "---\nid: 1\ntitle: A\nstatus: proposed\n---\nx")
	writeChange(t, dir, "active", "0002-b.md", "---\nid: 2\ntitle: B\nstatus: proposed\n---\nx")
	_ = a.Sync(ctx, st, planID, "/x")

	// Remove change 2 from disk → it must vanish on the next sync.
	if err := os.Remove(filepath.Join(dir, "active", "0002-b.md")); err != nil {
		t.Fatal(err)
	}
	_ = a.Sync(ctx, st, planID, "/x")

	if _, ok := itemByExt(t, st, planID, "docket:2"); ok {
		t.Fatal("docket:2 should have been reconciled away")
	}
	if _, ok := itemByExt(t, st, planID, "docket:1"); !ok {
		t.Fatal("docket:1 should still exist")
	}
}

func TestSyncLinks(t *testing.T) {
	st, planID, dir, a := setup(t)
	ctx := context.Background()
	writeChange(t, dir, "active", "0001-a.md", "---\nid: 1\ntitle: A\nstatus: proposed\n---\nx")
	writeChange(t, dir, "active", "0002-b.md", "---\nid: 2\ntitle: B\nstatus: proposed\ndepends_on: [1]\n---\nx")
	_ = a.Sync(ctx, st, planID, "/x")

	links, err := st.Links(ctx, planID)
	if err != nil {
		t.Fatal(err)
	}
	from2, _ := st.ItemIDByExtKey(ctx, planID, "docket:2")
	to1, _ := st.ItemIDByExtKey(ctx, planID, "docket:1")
	found := 0
	for _, l := range links {
		if l.From == from2 && l.To == to1 && l.Kind == "depends_on" {
			found++
		}
	}
	if found != 1 {
		t.Fatalf("expected exactly 1 depends_on link, got %d", found)
	}

	// Re-sync: AddLink is INSERT OR IGNORE, so no duplicate.
	_ = a.Sync(ctx, st, planID, "/x")
	links, _ = st.Links(ctx, planID)
	dup := 0
	for _, l := range links {
		if l.From == from2 && l.To == to1 {
			dup++
		}
	}
	if dup != 1 {
		t.Fatalf("link duplicated on re-sync: %d", dup)
	}
}
