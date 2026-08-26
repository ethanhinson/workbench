package adapter

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/ethanhinson/workbench/internal/store"
)

// Watch keeps a board in sync with a methodology's on-disk changes. It does an
// initial Sync immediately (so the board is populated on boot), then polls the
// change dir's coarse signature and re-runs Sync when it advances. It mirrors
// store.Watch's shape: blocks until ctx is cancelled, run it in a goroutine. A
// zero/negative interval defaults to 1s.
//
// A poller (not fsnotify) keeps zero new dependencies and matches the store's
// existing idiom. Because Sync is idempotent, a redundant sync is harmless, so the
// coarse {count, maxMtime} signature — which collapses a multi-file docket write
// into one resync — is all the debounce needed.
func Watch(ctx context.Context, a Adapter, st *store.Store, planID, repoDir string, interval time.Duration) {
	if interval <= 0 {
		interval = time.Second
	}
	dir, ok := a.ChangeDir(repoDir)
	if !ok {
		return
	}
	if err := a.Sync(ctx, st, planID, repoDir); err != nil {
		log.Printf("adapter: initial sync: %v", err)
	}
	last := dirSignature(dir)
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			sig := dirSignature(dir)
			if sig == last {
				continue
			}
			last = sig
			if err := a.Sync(ctx, st, planID, repoDir); err != nil {
				log.Printf("adapter: sync: %v", err)
			}
		}
	}
}

// dirSignature is a coarse change signal over the change dir's manifests: the file
// count and the newest mtime across active/ and archive/. Any add, remove, or edit
// advances it. It intentionally ignores content so a burst of edits within one
// tick collapses to a single resync.
func dirSignature(dir string) [2]int64 {
	var count, maxMod int64
	for _, sub := range []string{"active", "archive"} {
		entries, err := os.ReadDir(filepath.Join(dir, sub))
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			info, err := e.Info()
			if err != nil {
				continue
			}
			count++
			if m := info.ModTime().UnixNano(); m > maxMod {
				maxMod = m
			}
		}
	}
	return [2]int64{count, maxMod}
}
