// Package adapter projects an on-disk methodology footprint (docket change
// manifests, and later OpenSpec/Superpowers) onto a Workbench board, deterministically.
// It is the replacement for the live activity-feed sync: the files are the single
// source of truth, and Sync writes each item's real column_key/lane_key — the axis
// the renderer buckets by — so the board can never drift from the methodology state.
package adapter

import (
	"context"

	"github.com/ethanhinson/workbench/internal/store"
)

// Adapter projects a methodology's files onto a board.
type Adapter interface {
	// Name is the methodology key (e.g. "docket") — also the board profile.
	Name() string
	// Detect reports whether this methodology is present at repoDir.
	Detect(repoDir string) bool
	// ChangeDir returns the directory whose changes should be watched, and whether
	// it resolved. The filesystem watcher polls this dir.
	ChangeDir(repoDir string) (string, bool)
	// Sync reads repoDir, maps each artifact to a board item, and idempotently
	// upserts them onto planID (keyed by a stable ext_key). It reconciles deletes
	// (artifacts removed since the last sync). A malformed single file is skipped,
	// not fatal.
	Sync(ctx context.Context, st *store.Store, planID, repoDir string) error
}

// All returns the registered adapters in detection order.
func All() []Adapter { return []Adapter{NewDocketAdapter()} }

// Detect returns the first adapter whose methodology is present at repoDir.
func Detect(repoDir string) (Adapter, bool) {
	for _, a := range All() {
		if a.Detect(repoDir) {
			return a, true
		}
	}
	return nil, false
}
