package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

// TestWatchBridgesCrossProcessWrites is the core regression for the "browser
// doesn't update" bug: a write made by a *second* Store (a stand-in for another
// OS process on the same db file) must wake a subscriber on the *first* Store.
// The in-process broker alone can't do this; Watch polls PRAGMA data_version to
// bridge the gap.
func TestWatchBridgesCrossProcessWrites(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "shared.db")

	reader, err := Open(dbPath) // the "viz server" process
	if err != nil {
		t.Fatalf("open reader: %v", err)
	}
	t.Cleanup(func() { reader.Close() })

	writer, err := Open(dbPath) // the "MCP server" process
	if err != nil {
		t.Fatalf("open writer: %v", err)
	}
	t.Cleanup(func() { writer.Close() })

	ch, unsub := reader.Broker().Subscribe()
	defer unsub()

	// The reader watches the db for writes made by any connection.
	watchCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go reader.Watch(watchCtx, 20*time.Millisecond)

	// A write through the *other* Store — the reader's in-process broker never
	// sees this directly.
	if _, err := writer.CreatePlan(ctx, "Cross Process", "", "", "sdd"); err != nil {
		t.Fatalf("writer create: %v", err)
	}

	select {
	case <-ch:
		// Woken by the cross-process write, as intended.
	case <-time.After(2 * time.Second):
		t.Fatal("subscriber was not woken by a cross-process write within 2s")
	}
}

// TestDataVersionChangesOnForeignWrite pins the primitive Watch relies on:
// PRAGMA data_version, read on one connection, advances after another
// connection commits.
func TestDataVersionChangesOnForeignWrite(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "dv.db")

	a, err := Open(dbPath)
	if err != nil {
		t.Fatalf("open a: %v", err)
	}
	t.Cleanup(func() { a.Close() })
	b, err := Open(dbPath)
	if err != nil {
		t.Fatalf("open b: %v", err)
	}
	t.Cleanup(func() { b.Close() })

	before, err := a.DataVersion(ctx)
	if err != nil {
		t.Fatalf("data version: %v", err)
	}
	if _, err := b.CreatePlan(ctx, "Foreign", "", "", "sdd"); err != nil {
		t.Fatalf("b create: %v", err)
	}
	after, err := a.DataVersion(ctx)
	if err != nil {
		t.Fatalf("data version: %v", err)
	}
	if after == before {
		t.Fatalf("data_version did not change after a foreign write: %d", after)
	}
}
