package store

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ethanhinson/kanban-mcp/internal/board"
)

func newTestStore(t *testing.T) (*Store, *board.Plan) {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	p, err := st.EnsurePlan(context.Background(), "Test Plan", "")
	if err != nil {
		t.Fatalf("ensure plan: %v", err)
	}
	return st, p
}

func TestEnsurePlanSeedsDefaults(t *testing.T) {
	st, p := newTestStore(t)
	ctx := context.Background()

	cols, err := st.Columns(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(cols) != len(board.DefaultColumns()) {
		t.Fatalf("want %d columns, got %d", len(board.DefaultColumns()), len(cols))
	}
	if cols[0].Key != "backlog" || !cols[len(cols)-1].IsDone {
		t.Fatalf("unexpected column layout: %+v", cols)
	}

	// EnsurePlan must be idempotent: second call returns the same plan.
	p2, err := st.EnsurePlan(ctx, "Ignored", "")
	if err != nil {
		t.Fatal(err)
	}
	if p2.ID != p.ID {
		t.Fatalf("plan not idempotent: %s vs %s", p.ID, p2.ID)
	}
}

func TestNestedItemsAndMove(t *testing.T) {
	st, p := newTestStore(t)
	ctx := context.Background()

	epic, err := st.CreateItem(ctx, "a1", &board.Item{
		PlanID: p.ID, Kind: board.KindEpic, Title: "Auth", LaneKey: "shared",
	})
	if err != nil {
		t.Fatal(err)
	}
	story, err := st.CreateItem(ctx, "a1", &board.Item{
		PlanID: p.ID, ParentID: epic.ID, Kind: board.KindStory, Title: "Login", LaneKey: "shared",
		Labels: []board.Label{{NS: "priority", Value: "p0"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if story.ColumnKey != "backlog" {
		t.Fatalf("default column: got %q", story.ColumnKey)
	}

	// children filter
	kids, err := st.ListItems(ctx, p.ID, Filter{ParentID: epic.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(kids) != 1 || kids[0].ID != story.ID {
		t.Fatalf("nesting broken: %+v", kids)
	}

	if err := st.MoveItem(ctx, "a1", story.ID, "in_progress", ""); err != nil {
		t.Fatal(err)
	}
	moved, _ := st.ListItems(ctx, p.ID, Filter{ColumnKey: "in_progress"})
	if len(moved) != 1 {
		t.Fatalf("move failed: %+v", moved)
	}
}

func TestBadEnumsRejected(t *testing.T) {
	st, p := newTestStore(t)
	ctx := context.Background()

	if _, err := st.CreateItem(ctx, "a1", &board.Item{PlanID: p.ID, Kind: "chore", Title: "x"}); err == nil {
		t.Fatal("expected bad kind to be rejected")
	}
	if _, err := st.CreateItem(ctx, "a1", &board.Item{
		PlanID: p.ID, Kind: board.KindTask, Title: "x", ColumnKey: "backlog",
		Labels: []board.Label{{NS: "priority", Value: "urgent"}},
	}); err == nil {
		t.Fatal("expected bad label to be rejected")
	}
}

func TestWIPLimit(t *testing.T) {
	st, p := newTestStore(t)
	ctx := context.Background()

	// Set a WIP limit of 1 on in_progress directly.
	if _, err := st.db.ExecContext(ctx, `UPDATE column_def SET wip_limit=1 WHERE plan_id=? AND key='in_progress'`, p.ID); err != nil {
		t.Fatal(err)
	}
	a, _ := st.CreateItem(ctx, "a1", &board.Item{PlanID: p.ID, Kind: board.KindTask, Title: "a", ColumnKey: "in_progress", LaneKey: "shared"})
	if a == nil {
		t.Fatal("first in_progress item should be allowed")
	}
	b, _ := st.CreateItem(ctx, "a1", &board.Item{PlanID: p.ID, Kind: board.KindTask, Title: "b", LaneKey: "shared"})
	if err := st.MoveItem(ctx, "a1", b.ID, "in_progress", ""); err == nil {
		t.Fatal("expected WIP limit to block the move")
	}
}
