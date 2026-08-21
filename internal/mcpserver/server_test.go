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

// TestSpecGateThroughMCP drives the full spec-driven flow the way an agent does:
// create a story, hit the SDD gate on a premature move, set the spec via MCP,
// then complete the move — all through real tool calls over the wire.
func TestSpecGateThroughMCP(t *testing.T) {
	cs, _ := clientFor(t, "sdd")

	var created idResult
	call(t, cs, "item_create", map[string]any{
		"kind": "story", "title": "Login flow", "column": "specd",
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
	call(t, cs, "board_export", map[string]any{}, &snap, false)
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

	var epic idResult
	call(t, cs, "item_create", map[string]any{"kind": "epic", "title": "Auth", "labels": []string{"priority:p0"}}, &epic, false)
	var story idResult
	call(t, cs, "item_create", map[string]any{
		"kind": "story", "title": "Login", "parent_id": epic.ID,
	}, &story, false)

	var snap board.Snapshot
	call(t, cs, "board_export", map[string]any{}, &snap, false)
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
}
