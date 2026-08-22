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
	p, err := st.EnsurePlan(context.Background(), "Test Plan", "", "sdd")
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
	p2, err := st.EnsurePlan(ctx, "Ignored", "", "sdd")
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

func TestSDDSpecGate(t *testing.T) {
	st, p := newTestStore(t) // sdd profile
	ctx := context.Background()

	// A story in specd cannot leave until its spec is approved.
	story, err := st.CreateItem(ctx, "a1", &board.Item{
		PlanID: p.ID, Kind: board.KindStory, Title: "Login", ColumnKey: "specd", LaneKey: "shared",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.MoveItem(ctx, "a1", story.ID, "in_progress", ""); err == nil {
		t.Fatal("expected spec gate to block move out of specd")
	}
	// Approve the spec, then the same move succeeds.
	if err := st.SetSpec(ctx, "a1", story.ID, "specs/login.md", board.SpecApproved); err != nil {
		t.Fatal(err)
	}
	if err := st.MoveItem(ctx, "a1", story.ID, "in_progress", ""); err != nil {
		t.Fatalf("approved spec should allow move: %v", err)
	}
}

func TestKanbanExpediteBypassesLaneWIP(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "k.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	p, err := st.EnsurePlan(context.Background(), "K", "", "kanban")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	// standard lane WIP is 5; fill it, then confirm the 6th is rejected there...
	for i := 0; i < 5; i++ {
		if _, err := st.CreateItem(ctx, "a1", &board.Item{
			PlanID: p.ID, Kind: board.KindTask, Title: "t", ColumnKey: "backlog", LaneKey: "standard",
		}); err != nil {
			t.Fatalf("fill standard %d: %v", i, err)
		}
	}
	sixth, _ := st.CreateItem(ctx, "a1", &board.Item{
		PlanID: p.ID, Kind: board.KindTask, Title: "overflow", ColumnKey: "backlog", LaneKey: "standard",
	})
	if err := st.MoveItem(ctx, "a1", sixth.ID, "in_progress", "standard"); err == nil {
		t.Fatal("expected standard lane WIP to block")
	}
	// ...but moving it into the exempt expedite lane is allowed.
	if err := st.MoveItem(ctx, "a1", sixth.ID, "in_progress", "expedite"); err != nil {
		t.Fatalf("expedite lane should bypass WIP: %v", err)
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

// TestCreatePlanByNameIsolation proves many boards coexist in one db, are
// addressed independently, and CreatePlan is idempotent by name (select, not dup).
func TestCreatePlanByNameIsolation(t *testing.T) {
	st, first := newTestStore(t) // seeds "Test Plan"
	ctx := context.Background()

	a, err := st.CreatePlan(ctx, "Board A", "", "sdd")
	if err != nil {
		t.Fatal(err)
	}
	b, err := st.CreatePlan(ctx, "Board B", "", "kanban")
	if err != nil {
		t.Fatal(err)
	}
	if a.ID == b.ID || a.ID == first.ID {
		t.Fatal("distinct names must yield distinct boards")
	}

	// Idempotent by name: re-create "Board A" selects the same row.
	again, err := st.CreatePlan(ctx, "Board A", "", "scrum")
	if err != nil {
		t.Fatal(err)
	}
	if again.ID != a.ID {
		t.Fatalf("CreatePlan by existing name should select, got new id %s", again.ID)
	}

	// Items on A don't leak to B.
	st.CreateItem(ctx, "u", &board.Item{PlanID: a.ID, Kind: board.KindTask, Title: "on A", LaneKey: "shared"})
	itemsB, _ := st.ListItems(ctx, b.ID, Filter{})
	if len(itemsB) != 0 {
		t.Fatalf("board B should be empty, got %d", len(itemsB))
	}

	boards, _ := st.ListPlans(ctx)
	if len(boards) != 3 {
		t.Fatalf("want 3 boards (Test Plan + A + B), got %d", len(boards))
	}
}

// TestDeletePlanCascades proves deleting a board removes its items, links,
// labels, and events, leaves other boards untouched, and errors on a bad id.
func TestDeletePlanCascades(t *testing.T) {
	st, keep := newTestStore(t)
	ctx := context.Background()

	victim, _ := st.CreatePlan(ctx, "Victim", "", "sdd")
	i1, _ := st.CreateItem(ctx, "u", &board.Item{PlanID: victim.ID, Kind: board.KindStory, Title: "s1", LaneKey: "shared",
		Labels: []board.Label{{NS: "priority", Value: "p0"}}})
	i2, _ := st.CreateItem(ctx, "u", &board.Item{PlanID: victim.ID, Kind: board.KindTask, Title: "t1", LaneKey: "shared"})
	if err := st.AddLink(ctx, victim.ID, i2.ID, i1.ID, "depends_on"); err != nil {
		t.Fatal(err)
	}
	// An item on the board we keep, to prove isolation.
	keepItem, _ := st.CreateItem(ctx, "u", &board.Item{PlanID: keep.ID, Kind: board.KindTask, Title: "keep", LaneKey: "shared"})

	if err := st.DeletePlan(ctx, victim.ID); err != nil {
		t.Fatal(err)
	}

	// Board gone.
	if _, err := st.LoadPlan(ctx, victim.ID); err == nil {
		t.Fatal("deleted board should not load")
	}
	// Its rows gone (query the raw tables).
	for _, q := range []struct {
		table, where string
		arg          string
	}{
		{"item", "plan_id", victim.ID},
		{"link", "plan_id", victim.ID},
		{"event", "plan_id", victim.ID},
		{"lane", "plan_id", victim.ID},
		{"column_def", "plan_id", victim.ID},
		{"label", "item_id", i1.ID},
	} {
		var n int
		st.db.QueryRowContext(ctx, "SELECT COUNT(1) FROM "+q.table+" WHERE "+q.where+"=?", q.arg).Scan(&n)
		if n != 0 {
			t.Fatalf("%s rows for deleted board remain: %d", q.table, n)
		}
	}
	// The kept board is untouched.
	if _, err := st.LoadPlan(ctx, keep.ID); err != nil {
		t.Fatalf("kept board should survive: %v", err)
	}
	items, _ := st.ListItems(ctx, keep.ID, Filter{})
	if len(items) != 1 || items[0].ID != keepItem.ID {
		t.Fatalf("kept board's items should survive, got %+v", items)
	}

	// Deleting a nonexistent board errors.
	if err := st.DeletePlan(ctx, "nope"); err == nil {
		t.Fatal("deleting a missing board should error")
	}
}

// TestRenamePlan covers rename, the unique-name clash, same-name no-op, and a
// missing board.
func TestRenamePlan(t *testing.T) {
	st, keep := newTestStore(t)
	ctx := context.Background()
	other, _ := st.CreatePlan(ctx, "Other", "", "sdd")

	if err := st.RenamePlan(ctx, keep.ID, "Renamed"); err != nil {
		t.Fatal(err)
	}
	got, _ := st.LoadPlan(ctx, keep.ID)
	if got.Name != "Renamed" {
		t.Fatalf("name not updated, got %q", got.Name)
	}

	// Renaming to a name another board holds is rejected.
	if err := st.RenamePlan(ctx, keep.ID, "Other"); err == nil {
		t.Fatal("expected clash error renaming to an existing name")
	}
	// A board may keep its own name (no-op).
	if err := st.RenamePlan(ctx, other.ID, "Other"); err != nil {
		t.Fatalf("same-name rename should be a no-op, got: %v", err)
	}
	// Empty name and missing board are rejected.
	if err := st.RenamePlan(ctx, keep.ID, ""); err == nil {
		t.Fatal("expected error on empty name")
	}
	if err := st.RenamePlan(ctx, "nope", "Whatever"); err == nil {
		t.Fatal("expected error renaming a missing board")
	}
}
