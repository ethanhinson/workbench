package adapter

import (
	"context"
	"testing"
	"time"

	"github.com/ethanhinson/workbench/internal/store"
)

// Watch does an initial sync, then re-syncs when the change dir advances; it
// returns when its context is cancelled.
func TestWatch(t *testing.T) {
	st, planID, dir, a := setup(t)
	writeChange(t, dir, "active", "0001-a.md", "---\nid: 1\ntitle: A\nstatus: proposed\n---\nx")

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { Watch(ctx, a, st, planID, "/x", 20*time.Millisecond); close(done) }()

	// Initial sync should land change 1.
	waitForItems(t, st, planID, 1)

	// Add a second change → the watcher should pick it up.
	writeChange(t, dir, "active", "0002-b.md", "---\nid: 2\ntitle: B\nstatus: proposed\n---\nx")
	waitForItems(t, st, planID, 2)

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Watch did not return after context cancel")
	}
}

func waitForItems(t *testing.T, st *store.Store, planID string, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		items, _ := st.ListItems(context.Background(), planID, store.Filter{})
		if len(items) == want {
			return
		}
		time.Sleep(15 * time.Millisecond)
	}
	items, _ := st.ListItems(context.Background(), planID, store.Filter{})
	t.Fatalf("wanted %d items, have %d after waiting", want, len(items))
}
