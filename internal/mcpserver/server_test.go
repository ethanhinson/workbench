package mcpserver

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ethanhinson/kanban-mcp/internal/board"
	"github.com/ethanhinson/kanban-mcp/internal/store"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// clientFor spins up the real MCP server over an in-memory transport and returns
// a connected client session — no stdio, no shell, fully deterministic.
func clientFor(t *testing.T, profile string) (*mcp.ClientSession, *store.Store) {
	t.Helper()
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "it.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	srv, _, err := New(ctx, st, "Test Plan", "alice", profile)
	if err != nil {
		t.Fatal(err)
	}
	st1, ct := mcp.NewInMemoryTransports()
	if _, err := srv.Connect(ctx, st1, nil); err != nil {
		t.Fatal(err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	cs, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs, st
}

// call invokes a tool and decodes its structured JSON result into out.
// It fails the test if the tool returns an error result unless wantErr is set.
func call(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any, out any, wantErr bool) string {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s transport error: %v", name, err)
	}
	text := ""
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			text += tc.Text
		}
	}
	if res.IsError {
		if !wantErr {
			t.Fatalf("%s unexpectedly errored: %s", name, text)
		}
		return text
	}
	if wantErr {
		t.Fatalf("%s expected an error but succeeded: %s", name, text)
	}
	if out != nil && text != "" {
		if err := json.Unmarshal([]byte(text), out); err != nil {
			t.Fatalf("%s decode %q: %v", name, text, err)
		}
	}
	return text
}

type idResult struct {
	ID      string `json:"id"`
	Message string `json:"message"`
}

// startBoard calls board_start and returns the new board_id to thread through the
// item tools — the agent-facing "Start a board" entry point.
func startBoard(t *testing.T, cs *mcp.ClientSession, name, profile string) string {
	t.Helper()
	var out struct {
		BoardID string `json:"board_id"`
	}
	call(t, cs, "board_start", map[string]any{"name": name, "profile": profile}, &out, false)
	if out.BoardID == "" {
		t.Fatal("board_start returned no board_id")
	}
	return out.BoardID
}

// TestSpecGateThroughMCP drives the full spec-driven flow the way an agent does:
// create a story, hit the SDD gate on a premature move, set the spec via MCP,
// then complete the move — all through real tool calls over the wire.
func TestSpecGateThroughMCP(t *testing.T) {
	cs, _ := clientFor(t, "sdd")
	boardID := startBoard(t, cs, "Auth work", "sdd")

	var created idResult
	call(t, cs, "item_create", map[string]any{
		"board_id": boardID, "kind": "story", "title": "Login flow", "column": "specd",
	}, &created, false)
	if created.ID == "" {
		t.Fatal("item_create returned no id")
	}

	// Moving out of specd without an approved spec must be rejected by the gate.
	errText := call(t, cs, "item_move", map[string]any{
		"item_id": created.ID, "column": "in_progress",
	}, nil, true)
	if !strings.Contains(errText, "spec_status must be") {
		t.Fatalf("expected spec-gate rejection, got: %s", errText)
	}

	// Agent sets the spec through MCP...
	call(t, cs, "item_set_spec", map[string]any{
		"item_id": created.ID, "spec_ref": "specs/login.md", "status": "approved",
	}, nil, false)

	// ...now the same move is allowed.
	call(t, cs, "item_move", map[string]any{
		"item_id": created.ID, "column": "in_progress",
	}, nil, false)

	// Board reflects the final state.
	var snap board.Snapshot
	call(t, cs, "board_export", map[string]any{"board_id": boardID}, &snap, false)
	if len(snap.Items) != 1 {
		t.Fatalf("want 1 item, got %d", len(snap.Items))
	}
	it := snap.Items[0]
	if it.ColumnKey != "in_progress" || it.SpecStatus != board.SpecApproved || it.SpecRef != "specs/login.md" {
		t.Fatalf("unexpected final item: col=%s spec=%s ref=%s", it.ColumnKey, it.SpecStatus, it.SpecRef)
	}
}

// TestNestingAndExportThroughMCP proves epic>story nesting and the snapshot
// contract over the wire.
func TestNestingAndExportThroughMCP(t *testing.T) {
	cs, _ := clientFor(t, "sdd")
	boardID := startBoard(t, cs, "Auth epic", "sdd")

	var epic idResult
	call(t, cs, "item_create", map[string]any{"board_id": boardID, "kind": "epic", "title": "Auth", "labels": []string{"priority:p0"}}, &epic, false)
	var story idResult
	call(t, cs, "item_create", map[string]any{
		"board_id": boardID, "kind": "story", "title": "Login", "parent_id": epic.ID,
	}, &story, false)

	// Link the story to the epic as a first-class dependency (flat, not nesting).
	call(t, cs, "item_link", map[string]any{
		"board_id": boardID, "from_id": story.ID, "to_id": epic.ID, "kind": "depends_on",
	}, nil, false)

	var snap board.Snapshot
	call(t, cs, "board_export", map[string]any{"board_id": boardID}, &snap, false)
	if snap.SchemaVersion != board.SnapshotSchemaVersion {
		t.Fatalf("schema version: got %d", snap.SchemaVersion)
	}
	var found bool
	for _, it := range snap.Items {
		if it.ID == story.ID {
			found = it.ParentID == epic.ID
		}
	}
	if !found {
		t.Fatal("story not nested under epic in snapshot")
	}
	var hasLink bool
	for _, l := range snap.Links {
		if l.From == story.ID && l.To == epic.ID && l.Kind == "depends_on" {
			hasLink = true
		}
	}
	if !hasLink {
		t.Fatalf("item_link should have recorded a depends_on link; links=%+v", snap.Links)
	}
}

// TestBoardIsolation proves two boards in one db are independent: items on one
// don't leak into the other's export.
func TestBoardIsolation(t *testing.T) {
	cs, _ := clientFor(t, "sdd")
	a := startBoard(t, cs, "Board A", "sdd")
	b := startBoard(t, cs, "Board B", "kanban")

	call(t, cs, "item_create", map[string]any{"board_id": a, "kind": "task", "title": "only on A"}, nil, false)

	var snapB board.Snapshot
	call(t, cs, "board_export", map[string]any{"board_id": b}, &snapB, false)
	if len(snapB.Items) != 0 {
		t.Fatalf("board B should be empty, got %d items", len(snapB.Items))
	}

	// board_list sees both (plus the seeded default board from clientFor).
	var boards boardListOut
	call(t, cs, "board_list", map[string]any{}, &boards, false)
	if len(boards.Boards) < 2 {
		t.Fatalf("board_list should see at least 2 boards, got %d", len(boards.Boards))
	}

	// A tool call with a bogus board_id is rejected with guidance.
	errText := call(t, cs, "item_create", map[string]any{"board_id": "nope", "kind": "task", "title": "x"}, nil, true)
	if !strings.Contains(errText, "board_start") {
		t.Fatalf("expected board_start guidance, got: %s", errText)
	}
}

// TestNoSeedWhenPlanEmpty proves that starting the server without a plan name
// seeds NO board — the runtime-board model, no empty placeholder left behind.
func TestNoSeedWhenPlanEmpty(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "it.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	srv, s, err := New(ctx, st, "", "alice", "sdd")
	if err != nil {
		t.Fatal(err)
	}
	if s.PlanID() != "" {
		t.Fatalf("empty plan name must seed no board, got PlanID %q", s.PlanID())
	}
	boards, _ := st.ListPlans(ctx)
	if len(boards) != 0 {
		t.Fatalf("no board should exist before board_start, got %d", len(boards))
	}

	// The agent creates its own board at runtime.
	st1, ct := mcp.NewInMemoryTransports()
	srv.Connect(ctx, st1, nil)
	client := mcp.NewClient(&mcp.Implementation{Name: "t", Version: "1"}, nil)
	cs, _ := client.Connect(ctx, ct, nil)
	t.Cleanup(func() { cs.Close() })
	startBoard(t, cs, "runtime board", "sdd")
	if boards, _ := st.ListPlans(ctx); len(boards) != 1 {
		t.Fatalf("board_start should create exactly one board, got %d", len(boards))
	}
}

// TestBoardDeleteThroughMCP proves board_delete removes a board (and its items)
// over the wire, and that operating on a deleted board is then rejected.
func TestBoardDeleteThroughMCP(t *testing.T) {
	cs, _ := clientFor(t, "sdd")
	keep := startBoard(t, cs, "Keeper", "sdd")
	victim := startBoard(t, cs, "Throwaway", "sdd")
	call(t, cs, "item_create", map[string]any{"board_id": victim, "kind": "task", "title": "temp"}, nil, false)

	var before boardListOut
	call(t, cs, "board_list", map[string]any{}, &before, false)

	var del idResult
	call(t, cs, "board_delete", map[string]any{"board_id": victim}, &del, false)
	if !strings.Contains(del.Message, "Throwaway") {
		t.Fatalf("unexpected delete message: %s", del.Message)
	}

	// Gone from the list.
	var after boardListOut
	call(t, cs, "board_list", map[string]any{}, &after, false)
	if len(after.Boards) != len(before.Boards)-1 {
		t.Fatalf("board_list should shrink by 1: before=%d after=%d", len(before.Boards), len(after.Boards))
	}
	for _, b := range after.Boards {
		if b.ID == victim {
			t.Fatal("deleted board still listed")
		}
	}

	// Operating on the deleted board is rejected; the kept board still works.
	errText := call(t, cs, "board_export", map[string]any{"board_id": victim}, nil, true)
	if !strings.Contains(errText, "board_start") {
		t.Fatalf("expected rejection for deleted board, got: %s", errText)
	}
	call(t, cs, "board_export", map[string]any{"board_id": keep}, &board.Snapshot{}, false)

	// Deleting a nonexistent board is rejected.
	call(t, cs, "board_delete", map[string]any{"board_id": "nope"}, nil, true)
}
