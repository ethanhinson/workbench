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
	// It should still exist on the board (as an activity item).
	items, _ := st.ListItems(ctx, plan.ID, store.Filter{})
	got := 0
	for _, it := range items {
		if it.IsActivity() {
			got++
		}
	}
	if got != 1 {
		t.Fatalf("expected 1 activity item on the project board, got %d", got)
	}
}
