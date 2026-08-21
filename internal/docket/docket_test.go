package docket

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ethanhinson/kanban-mcp/internal/board"
	"github.com/ethanhinson/kanban-mcp/internal/store"
)

const manifestProposed = `---
id: 74
slug: sandbox-health-emitter
title: sandbox health emitter
status: proposed
priority: medium
type: feat
spec:
plan:
depends_on: [63]
discovered_from: [63]
blocked_by:
trivial: false
---
body
`

const manifestParent = `---
id: 63
slug: bash-container-substrate
title: bash container substrate
status: done
priority: high
type: feat
spec: docs/specs/63.md
plan: docs/plans/63.md
results: docs/results/63.md
---
`

const manifestFix = `---
id: 40
slug: some-bug
title: a fix
status: proposed
priority: low
type: fix
spec: docs/specs/40.md
plan: docs/plans/40.md
branch: fix/some-bug
---
`

func writeDocket(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	active := filepath.Join(root, "changes", "active")
	archive := filepath.Join(root, "changes", "archive")
	for _, d := range []string{active, archive} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write := func(dir, name, content string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(active, "0074-x.md", manifestProposed)
	write(archive, "0063-y.md", manifestParent)
	write(active, "0040-z.md", manifestFix)
	return root
}

func TestImportMapping(t *testing.T) {
	changes, err := Import(writeDocket(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 3 {
		t.Fatalf("want 3 changes, got %d", len(changes))
	}
	byID := map[int]Change{}
	for _, c := range changes {
		byID[c.ID] = c
	}

	// #74 proposed, no spec/plan => backlog, needs-brainstorm, missing spec, feat lane.
	c74 := byID[74]
	if got := c74.ColumnFor(); got != "backlog" {
		t.Errorf("#74 column: got %q want backlog", got)
	}
	if got := c74.SpecStatusFor(); got != board.SpecMissing {
		t.Errorf("#74 spec: got %q", got)
	}
	if got := c74.PriorityFor(); got != board.PriorityP1 {
		t.Errorf("#74 priority: got %q", got)
	}
	if got := c74.LaneFor(); got != "feat" {
		t.Errorf("#74 lane: got %q", got)
	}

	// #63 done => done column, approved spec.
	if got := byID[63].ColumnFor(); got != "done" {
		t.Errorf("#63 column: got %q want done", got)
	}
	if got := byID[63].SpecStatusFor(); got != board.SpecApproved {
		t.Errorf("#63 spec: got %q want approved", got)
	}

	// #40 proposed + branch set => in_progress; fix type => bug kind, fix lane.
	if got := byID[40].ColumnFor(); got != "in_progress" {
		t.Errorf("#40 column: got %q want in_progress", got)
	}
	if got := kindFor(byID[40]); got != board.KindBug {
		t.Errorf("#40 kind: got %q want bug", got)
	}
}

func TestSyncIdempotentAndNested(t *testing.T) {
	root := writeDocket(t)
	st, err := store.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	plan, _ := st.EnsurePlan(ctx, "P", "", "docket")

	syncer := NewSyncer(st, plan.ID)
	r1, err := syncer.Sync(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	if r1.Changes != 3 {
		t.Fatalf("first sync changes: %d", r1.Changes)
	}
	items1, _ := st.ListItems(ctx, plan.ID, store.Filter{})
	if len(items1) != 3 {
		t.Fatalf("want 3 items after sync, got %d", len(items1))
	}

	// Re-sync must not duplicate (idempotent by ext key).
	if _, err := syncer.Sync(ctx, root); err != nil {
		t.Fatal(err)
	}
	items2, _ := st.ListItems(ctx, plan.ID, store.Filter{})
	if len(items2) != 3 {
		t.Fatalf("re-sync duplicated items: got %d want 3", len(items2))
	}

	// #74 discovered_from [63] and 63 is imported => nested under it.
	var c74 *board.Item
	for i := range items2 {
		if items2[i].ExtKey == "docket:74" {
			c74 = &items2[i]
		}
	}
	if c74 == nil {
		t.Fatal("missing docket:74 item")
	}
	parentID, ok := st.ItemIDByExtKey(ctx, plan.ID, "docket:63")
	if !ok || c74.ParentID != parentID {
		t.Fatalf("#74 not nested under #63: parent=%q", c74.ParentID)
	}
}
