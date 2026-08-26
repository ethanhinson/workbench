package viz

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ethanhinson/workbench/internal/store"
)

func TestCleanCommand(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		// The common harness wrapper: cd into the repo, then run the real command.
		{"cd /Users/x/dev/repo && go test ./...", "go test ./..."},
		{"cd /a/b && git status --short", "git status --short"},
		// Semicolon connector.
		{"cd /a/b ; ls -la", "ls -la"},
		// Nested cds (some wrappers stack them) collapse to the final command.
		{"cd /a && cd /b && echo hi", "echo hi"},
		// A bare cd with no following command stays intact (still reads as something).
		{"cd /Users/x/dev/repo", "cd /Users/x/dev/repo"},
		// No wrapper: unchanged.
		{"go build ./...", "go build ./..."},
		// Leading/trailing space is trimmed.
		{"  cd /a &&   ls  ", "ls"},
		// Not a cd prefix (a command that merely contains 'cd' later) is untouched.
		{"echo cd /a && ls", "echo cd /a && ls"},
	}
	for _, c := range cases {
		if got := cleanCommand(c.in); got != c.want {
			t.Errorf("cleanCommand(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

const actBody = `{"harness":"claude-code","event_type":"tool_use_complete","session_id":"s1","tool":"Bash","target":"go test"}`

func postActivity(t *testing.T, srv *httptest.Server, query string) {
	t.Helper()
	resp, err := http.Post(srv.URL+"/api/activity"+query, "application/json", strings.NewReader(actBody))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode >= 300 { // always acked 2xx, even when dropped
		t.Fatalf("activity POST status %d", resp.StatusCode)
	}
}

func workCount(t *testing.T, st *store.Store, planID string) int {
	t.Helper()
	snap, err := st.Snapshot(context.Background(), planID)
	if err != nil {
		t.Fatal(err)
	}
	return snap.Stats.TotalItems
}

// An activity POST that names no board must be DROPPED, not projected onto the
// default board — otherwise a session's tool-call log pollutes an unrelated board.
func TestActivityWithoutBoardIsDropped(t *testing.T) {
	st, _ := store.Open(t.TempDir() + "/a.db")
	defer st.Close()
	plan, _ := st.CreatePlan(context.Background(), "P", "/proj/x", "", "docket")

	srv := httptest.NewServer(NewServer(st, plan.ID).Handler())
	defer srv.Close()

	postActivity(t, srv, "") // no ?board / ?project
	if n := workCount(t, st, plan.ID); n != 0 {
		t.Fatalf("default board gained %d items from an unaddressed activity event; want 0", n)
	}
}

// ?project= routes an activity event to that project's board.
func TestActivityRoutesByProject(t *testing.T) {
	st, _ := store.Open(t.TempDir() + "/a.db")
	defer st.Close()
	ctx := context.Background()
	plan, _ := st.CreatePlan(ctx, "P", "/proj/x", "", "docket")

	srv := httptest.NewServer(NewServer(st, "").Handler())
	defer srv.Close()

	postActivity(t, srv, "?project=/proj/x")
	// Projected — but as an activity event, so it must NOT count as work.
	if n := workCount(t, st, plan.ID); n != 0 {
		t.Fatalf("activity event counted as work: got %d, want 0", n)
	}
	if got := activityCount(t, st, plan.ID); got != 1 {
		t.Fatalf("expected 1 activity item on the project board, got %d", got)
	}
}

// activityCount reports how many activity events landed on a board. Activity now
// lives in the passive event log (off the item table), so it's counted via
// ListActivity, never as board items.
func activityCount(t *testing.T, st *store.Store, planID string) int {
	t.Helper()
	acts, err := st.ListActivity(context.Background(), planID, 0)
	if err != nil {
		t.Fatal(err)
	}
	return len(acts)
}

func postJSON(t *testing.T, srv *httptest.Server, query, body string) {
	t.Helper()
	resp, err := http.Post(srv.URL+"/api/activity"+query, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode >= 300 {
		t.Fatalf("activity POST status %d", resp.StatusCode)
	}
}

// Each supported harness's NATIVE hook payload — with its own session-id and
// project field names — must route to the project board via the same seam. This is
// the harness-agnostic contract: the server normalizes on ingest.
func TestNativeHookRoutingByHarness(t *testing.T) {
	cases := []struct{ name, body string }{
		{"claude", `{"hook_event_name":"PostToolUse","session_id":"c1","cwd":"/proj/x","tool_name":"Bash","tool_input":{"command":"go test"}}`},
		{"codex", `{"hook_event_name":"PostToolUse","session_id":"o1","cwd":"/proj/x","tool_name":"apply_patch"}`},
		{"cursor", `{"hook_event_name":"afterShellExecution","conversation_id":"u1","workspace_roots":["/proj/x"],"tool_name":"shell"}`},
		{"grok", `{"hookEventName":"PostToolUse","sessionId":"g1","workspaceRoot":"/proj/x","toolName":"Bash"}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			st, _ := store.Open(t.TempDir() + "/a.db")
			defer st.Close()
			plan, _ := st.CreatePlan(context.Background(), "P", "/proj/x", "", "docket")
			srv := httptest.NewServer(NewServer(st, "").Handler())
			defer srv.Close()

			postJSON(t, srv, "", c.body) // no query — project must come from the payload
			if got := activityCount(t, st, plan.ID); got != 1 {
				t.Fatalf("%s: expected 1 activity item routed by payload project, got %d", c.name, got)
			}
		})
	}
}

// The first event for a session seeds session→board from its project; subsequent
// events route by the learned mapping even when they carry NO project field.
func TestActivitySessionLearningSticks(t *testing.T) {
	st, _ := store.Open(t.TempDir() + "/a.db")
	defer st.Close()
	ctx := context.Background()
	plan, _ := st.CreatePlan(ctx, "P", "/proj/x", "", "docket")
	srv := httptest.NewServer(NewServer(st, "").Handler())
	defer srv.Close()

	// First event carries the project → seeds the map.
	postJSON(t, srv, "", `{"hook_event_name":"PostToolUse","session_id":"s9","cwd":"/proj/x","tool_name":"Bash"}`)
	if _, ok := st.BoardForSession(ctx, "s9"); !ok {
		t.Fatal("session s9 was not learned after its first project-bearing event")
	}
	// Second event has NO project — must still land via the learned mapping.
	postJSON(t, srv, "", `{"hook_event_name":"PostToolUse","session_id":"s9","tool_name":"Grep"}`)
	if got := activityCount(t, st, plan.ID); got != 2 {
		t.Fatalf("expected 2 activity items (learned routing), got %d", got)
	}
}

// When a project has multiple boards, activity seeds to the most recently created.
func TestActivityMultiBoardPicksNewest(t *testing.T) {
	st, _ := store.Open(t.TempDir() + "/a.db")
	defer st.Close()
	ctx := context.Background()
	st.CreatePlan(ctx, "old", "/proj/x", "", "docket")
	newest, _ := st.CreatePlan(ctx, "new", "/proj/x", "", "sdd")
	srv := httptest.NewServer(NewServer(st, "").Handler())
	defer srv.Close()

	postJSON(t, srv, "", `{"hook_event_name":"PostToolUse","session_id":"m1","cwd":"/proj/x","tool_name":"Bash"}`)
	got, ok := st.BoardForSession(ctx, "m1")
	if !ok || got != newest.ID {
		t.Fatalf("multi-board project: session mapped to %q, want newest %q", got, newest.ID)
	}
}
