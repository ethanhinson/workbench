package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/ethanhinson/kanban-mcp/internal/board"
)

// TestItemDetailWithLinksNoDeadlock guards the MaxOpenConns(1) hazard: resolving
// link refs while a rows cursor is open would deadlock. ItemDetail must drain
// cursors before resolving. A hang here fails via the test timeout.
func TestItemDetailWithLinksNoDeadlock(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "d.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	p, _ := st.CreatePlan(ctx, "P", "", "", "docket")

	a, _ := st.CreateItem(ctx, "x", &board.Item{PlanID: p.ID, Kind: board.KindStory, Title: "#1 A", ColumnKey: "backlog", LaneKey: "feat"})
	b, _ := st.CreateItem(ctx, "x", &board.Item{PlanID: p.ID, Kind: board.KindStory, Title: "#2 B", ColumnKey: "backlog", LaneKey: "feat"})
	c, _ := st.CreateItem(ctx, "x", &board.Item{PlanID: p.ID, Kind: board.KindStory, Title: "#3 C", ColumnKey: "backlog", LaneKey: "feat"})
	// b depends on a; c depends on b.
	st.AddLink(ctx, p.ID, b.ID, a.ID, "depends_on")
	st.AddLink(ctx, p.ID, c.ID, b.ID, "depends_on")

	done := make(chan struct{})
	var detail board.ItemDetail
	go func() {
		detail, err = st.ItemDetail(ctx, p.ID, b.ID)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("ItemDetail deadlocked (rows cursor held across ref queries)")
	}
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.DependsOn) != 1 || detail.DependsOn[0].ID != a.ID {
		t.Fatalf("b should depend on a: %+v", detail.DependsOn)
	}
	if len(detail.DependedBy) != 1 || detail.DependedBy[0].ID != c.ID {
		t.Fatalf("b should be depended-on by c: %+v", detail.DependedBy)
	}
}
