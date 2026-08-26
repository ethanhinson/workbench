package mcpserver

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ethanhinson/workbench/internal/board"
	"github.com/ethanhinson/workbench/internal/store"
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

	srv, _ := New(st, "alice", profile, "testproj")
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

	// board_list sees exactly the two boards started here (the server seeds none).
	var boards boardListOut
	call(t, cs, "board_list", map[string]any{}, &boards, false)
	if len(boards.Boards) != 2 {
		t.Fatalf("board_list should see 2 boards, got %d", len(boards.Boards))
	}

	// A tool call with a bogus board_id is rejected with guidance.
	errText := call(t, cs, "item_create", map[string]any{"board_id": "nope", "kind": "task", "title": "x"}, nil, true)
	if !strings.Contains(errText, "board_start") {
		t.Fatalf("expected board_start guidance, got: %s", errText)
	}
}

// TestBoardsByProject proves boards are scoped by project: the same board name
// coexists under different projects, board_start returns the project, and
// board_list filters by project (and falls back to the server default project).
func TestBoardsByProject(t *testing.T) {
	cs, _ := clientFor(t, "sdd") // server default project = "testproj"

	// Same name, two explicit projects → two distinct boards.
	var a, b boardStartOut
	call(t, cs, "board_start", map[string]any{"name": "auth work", "project": "/dev/alpha"}, &a, false)
	call(t, cs, "board_start", map[string]any{"name": "auth work", "project": "/dev/beta"}, &b, false)
	if a.BoardID == b.BoardID {
		t.Fatal("same name under different projects must be distinct boards")
	}
	if a.Project != "/dev/alpha" || b.Project != "/dev/beta" {
		t.Fatalf("board_start should echo project: %q %q", a.Project, b.Project)
	}

	// Re-starting the same (project,name) selects the existing board.
	var again boardStartOut
	call(t, cs, "board_start", map[string]any{"name": "auth work", "project": "/dev/alpha"}, &again, false)
	if again.BoardID != a.BoardID {
		t.Fatal("board_start must be idempotent per (project,name)")
	}

	// A board with no explicit project lands in the server default project.
	var d boardStartOut
	call(t, cs, "board_start", map[string]any{"name": "default-proj board"}, &d, false)
	if d.Project != "testproj" {
		t.Fatalf("board should default to server project, got %q", d.Project)
	}

	// board_list filtered by project returns only that project's boards.
	var alpha boardListOut
	call(t, cs, "board_list", map[string]any{"project": "/dev/alpha"}, &alpha, false)
	if len(alpha.Boards) != 1 || alpha.Boards[0].ID != a.BoardID {
		t.Fatalf("project filter should return only /dev/alpha's board, got %+v", alpha.Boards)
	}

	// Unfiltered board_list spans projects.
	var all boardListOut
	call(t, cs, "board_list", map[string]any{}, &all, false)
	projects := map[string]bool{}
	for _, bd := range all.Boards {
		projects[bd.Project] = true
	}
	if !projects["/dev/alpha"] || !projects["/dev/beta"] || !projects["testproj"] {
		t.Fatalf("unfiltered list should span projects, saw %v", projects)
	}
}

// TestServerSeedsNoBoard proves the server creates no board at construction — the
// runtime-board model, where the db is empty until the agent calls board_start.
func TestServerSeedsNoBoard(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "it.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	srv, _ := New(st, "alice", "sdd", "testproj")
	if boards, _ := st.ListPlans(ctx); len(boards) != 0 {
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

// TestBoardRenameThroughMCP proves board_rename updates the name over the wire,
// reflects in board_list, and rejects a duplicate name and an unknown board.
func TestBoardRenameThroughMCP(t *testing.T) {
	cs, _ := clientFor(t, "sdd")
	a := startBoard(t, cs, "Old Name", "sdd")
	startBoard(t, cs, "Taken", "sdd")

	var out idResult
	call(t, cs, "board_rename", map[string]any{"board_id": a, "name": "New Name"}, &out, false)
	if !strings.Contains(out.Message, "New Name") {
		t.Fatalf("unexpected rename message: %s", out.Message)
	}

	var boards boardListOut
	call(t, cs, "board_list", map[string]any{}, &boards, false)
	var found bool
	for _, b := range boards.Boards {
		if b.ID == a {
			if b.Name != "New Name" {
				t.Fatalf("board_list shows stale name %q", b.Name)
			}
			found = true
		}
	}
	if !found {
		t.Fatal("renamed board missing from board_list")
	}

	// Renaming to an existing name is rejected.
	errText := call(t, cs, "board_rename", map[string]any{"board_id": a, "name": "Taken"}, nil, true)
	if !strings.Contains(errText, "already exists") {
		t.Fatalf("expected duplicate-name rejection, got: %s", errText)
	}
	// Renaming an unknown board is rejected.
	call(t, cs, "board_rename", map[string]any{"board_id": "nope", "name": "X"}, nil, true)
}

// TestBoardSetProjectThroughMCP proves board_set_project moves a board between
// projects over the wire, board_list reflects the new project, and a name clash in
// the target project is rejected.
func TestBoardSetProjectThroughMCP(t *testing.T) {
	cs, _ := clientFor(t, "sdd")
	var mover, blocker boardStartOut
	call(t, cs, "board_start", map[string]any{"name": "shared", "project": "/p/one"}, &mover, false)
	call(t, cs, "board_start", map[string]any{"name": "shared", "project": "/p/two"}, &blocker, false)

	// Move to a free project — allowed, reflected in board_list.
	var out idResult
	call(t, cs, "board_set_project", map[string]any{"board_id": mover.BoardID, "project": "/p/three"}, &out, false)
	if !strings.Contains(out.Message, "/p/three") {
		t.Fatalf("unexpected message: %s", out.Message)
	}
	var boards boardListOut
	call(t, cs, "board_list", map[string]any{"project": "/p/three"}, &boards, false)
	if len(boards.Boards) != 1 || boards.Boards[0].ID != mover.BoardID {
		t.Fatalf("board should be listed under /p/three, got %+v", boards.Boards)
	}

	// Moving into a project that already has a "shared" board is rejected.
	errText := call(t, cs, "board_set_project", map[string]any{"board_id": mover.BoardID, "project": "/p/two"}, nil, true)
	if !strings.Contains(errText, "already has a board") {
		t.Fatalf("expected clash rejection, got: %s", errText)
	}
	// Unknown board is rejected.
	call(t, cs, "board_set_project", map[string]any{"board_id": "nope", "project": "/p/x"}, nil, true)
}

// TestItemUpsertAndContentThroughMCP proves item_upsert is idempotent by ext_key,
// places the card by its real column (column-driven placement) and carries a
// group chip, and item_set_content updates content.
func TestItemUpsertAndContentThroughMCP(t *testing.T) {
	cs, _ := clientFor(t, "sdd")
	b := startBoard(t, cs, "OpenSpec board", "sdd")

	// First upsert creates the card in a real column with a group chip.
	var first idResult
	call(t, cs, "item_upsert", map[string]any{
		"board_id": b, "ext_key": "openspec:auth", "title": "Auth change",
		"content": "# Auth\nproposal body", "column": "specifying", "labels": []string{"group:auth"},
	}, &first, false)
	if first.ID == "" {
		t.Fatal("upsert returned no id")
	}

	// Re-upsert with the same ext_key updates in place (same id, no duplicate) and
	// moves the card to a new column.
	var second idResult
	call(t, cs, "item_upsert", map[string]any{
		"board_id": b, "ext_key": "openspec:auth", "title": "Auth change (v2)",
		"content": "# Auth\nrevised", "column": "specd", "labels": []string{"group:auth"},
	}, &second, false)
	if second.ID != first.ID {
		t.Fatalf("upsert should update in place: %s vs %s", first.ID, second.ID)
	}

	// Exactly one item, with the updated content + title.
	var snap board.Snapshot
	call(t, cs, "board_export", map[string]any{"board_id": b}, &snap, false)
	if len(snap.Items) != 1 {
		t.Fatalf("upsert duplicated: %d items", len(snap.Items))
	}
	it := snap.Items[0]
	if it.Title != "Auth change (v2)" || it.Content != "# Auth\nrevised" {
		t.Fatalf("upsert did not update fields: %+v", it)
	}
	// Placement is the real column, and the group chip survives.
	if it.ColumnKey != "specd" {
		t.Fatalf("column-driven placement wrong: column_key=%q, want specd", it.ColumnKey)
	}
	var hasGroup bool
	for _, l := range it.Labels {
		if l.NS == "group" && l.Value == "auth" {
			hasGroup = true
		}
	}
	if !hasGroup {
		t.Fatalf("group chip missing: %+v", it.Labels)
	}

	// item_set_content updates just the content.
	call(t, cs, "item_set_content", map[string]any{"item_id": first.ID, "content": "# Auth\nfinal"}, nil, false)
	call(t, cs, "board_export", map[string]any{"board_id": b}, &snap, false)
	if snap.Items[0].Content != "# Auth\nfinal" {
		t.Fatalf("set_content did not update: %q", snap.Items[0].Content)
	}
}

// TestSnapshotCarriesLayout proves the snapshot ships the board's layout + has_layout
// and that item content round-trips into the export.
func TestSnapshotCarriesLayout(t *testing.T) {
	cs, _ := clientFor(t, "sdd")
	b := startBoard(t, cs, "Layout board", "sdd")

	// No layout yet.
	var snap board.Snapshot
	call(t, cs, "board_export", map[string]any{"board_id": b}, &snap, false)
	if snap.HasLayout {
		t.Fatal("fresh board should report has_layout=false")
	}
	if snap.SchemaVersion != board.SnapshotSchemaVersion {
		t.Fatalf("schema version: got %d want %d", snap.SchemaVersion, board.SnapshotSchemaVersion)
	}

	// Set a layout, then it appears in the snapshot.
	call(t, cs, "board_set_layout", map[string]any{
		"board_id": b,
		"layout": map[string]any{
			"nav": []map[string]any{{"id": "specs", "label": "Specs", "view": "specs"}},
			"views": map[string]any{
				"specs": map[string]any{"type": "doc"},
			},
		},
	}, nil, false)

	call(t, cs, "board_export", map[string]any{"board_id": b}, &snap, false)
	if !snap.HasLayout {
		t.Fatal("board_export should report has_layout=true after set_layout")
	}
	if len(snap.Layout.Nav) != 1 || snap.Layout.Nav[0].ID != "specs" || snap.Layout.Views["specs"].Type != board.ViewDoc {
		t.Fatalf("snapshot layout wrong: %+v", snap.Layout)
	}
}

// TestItemUpsertValidatesLabels proves item_upsert rejects a malformed label
// namespace but accepts valid ones (group: the card chip; view/lane/column remain
// valid open namespaces even though they no longer drive placement).
func TestItemUpsertValidatesLabels(t *testing.T) {
	cs, _ := clientFor(t, "sdd")
	b := startBoard(t, cs, "Validate board", "sdd")

	// Bogus namespace → rejected.
	errText := call(t, cs, "item_upsert", map[string]any{
		"board_id": b, "ext_key": "x:1", "title": "t", "labels": []string{"bogusns:v"},
	}, nil, true)
	if !strings.Contains(errText, "namespace") {
		t.Fatalf("expected namespace rejection, got: %s", errText)
	}
	// Valid labels → accepted (group is the chip; view/lane stay valid-but-inert).
	call(t, cs, "item_upsert", map[string]any{
		"board_id": b, "ext_key": "x:2", "title": "ok", "labels": []string{"group:auth", "view:specs", "lane:a"},
	}, nil, false)
}
