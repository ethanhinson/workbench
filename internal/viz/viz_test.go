package viz

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ethanhinson/kanban-mcp/internal/board"
	"github.com/ethanhinson/kanban-mcp/internal/store"
)

// TestSSEPushesOnMutation proves the store broker -> SSE path: a connected client
// gets an initial frame, then another frame after a store mutation, with no poll.
func TestSSEPushesOnMutation(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/v.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	plan, _ := st.EnsurePlan(ctx, "P", "", "sdd")

	srv := httptest.NewServer(NewServer(st, plan.ID).Handler())
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/stream", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("content-type: %q", ct)
	}

	frames := make(chan string, 4)
	go func() {
		sc := bufio.NewScanner(resp.Body)
		var data string
		for sc.Scan() {
			line := sc.Text()
			if line == "" {
				if data != "" {
					frames <- data
					data = ""
				}
				continue
			}
			if strings.HasPrefix(line, "data: ") {
				data += strings.TrimPrefix(line, "data: ")
			}
		}
	}()

	// Frame 1: initial.
	select {
	case <-frames:
	case <-time.After(2 * time.Second):
		t.Fatal("no initial SSE frame")
	}

	// Mutate the store; the broker should push frame 2.
	if _, err := st.CreateItem(ctx, "a1", &board.Item{
		PlanID: plan.ID, Kind: board.KindStory, Title: "New", ColumnKey: "backlog", LaneKey: "shared",
	}); err != nil {
		t.Fatal(err)
	}

	select {
	case f := <-frames:
		if !strings.Contains(f, "New") {
			t.Fatalf("push frame missing new item: %.80s", f)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no SSE push after mutation")
	}
}
