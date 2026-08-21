package docket

import (
	"context"
	"fmt"

	"github.com/ethanhinson/kanban-mcp/internal/board"
	"github.com/ethanhinson/kanban-mcp/internal/store"
)

// Syncer imports docket changes into a kanban plan idempotently.
type Syncer struct {
	st     *store.Store
	planID string
}

func NewSyncer(st *store.Store, planID string) *Syncer { return &Syncer{st: st, planID: planID} }

// Result summarizes a sync run.
type Result struct {
	Changes int
	Lanes   int
	Links   int
}

// Sync reads the docket docs directory and upserts every change as a board item.
// Each change becomes a card keyed by "docket:<id>"; re-running updates in place.
// Lanes are created per docket type (feat/fix/...). Nesting: a change with a
// single depends_on parent is nested under it when that parent is also imported.
func (s *Syncer) Sync(ctx context.Context, docsDir string) (Result, error) {
	changes, err := Import(docsDir)
	if err != nil {
		return Result{}, err
	}

	// First pass: ensure a lane exists for every distinct type.
	seenLane := map[string]bool{}
	laneCount := 0
	for _, c := range changes {
		lane := c.LaneFor()
		if lane == "shared" || seenLane[lane] {
			continue
		}
		seenLane[lane] = true
		if _, err := s.st.EnsureLane(ctx, s.planID, board.Lane{Key: lane, Name: lane}); err != nil {
			return Result{}, fmt.Errorf("ensure lane %q: %w", lane, err)
		}
		laneCount++
	}

	// Second pass: upsert all items so ext keys exist (flat — no parent nesting).
	for _, c := range changes {
		if _, err := s.upsert(ctx, c); err != nil {
			return Result{}, err
		}
	}

	// Third pass: record dependency links now that every card exists. Flat board
	// shows these as links instead of containment.
	linkCount := 0
	addLink := func(fromID string, toIDs []int, kind string) {
		for _, n := range toIDs {
			toID, ok := s.st.ItemIDByExtKey(ctx, s.planID, fmt.Sprintf("docket:%d", n))
			if !ok {
				continue
			}
			if err := s.st.AddLink(ctx, s.planID, fromID, toID, kind); err == nil {
				linkCount++
			}
		}
	}
	for _, c := range changes {
		fromID, ok := s.st.ItemIDByExtKey(ctx, s.planID, fmt.Sprintf("docket:%d", c.ID))
		if !ok {
			continue
		}
		addLink(fromID, c.DependsOn, "depends_on")
		addLink(fromID, c.DiscoveredFrom, "discovered_from")
		addLink(fromID, c.Related, "related")
	}

	return Result{Changes: len(changes), Lanes: laneCount, Links: linkCount}, nil
}

func (s *Syncer) upsert(ctx context.Context, c Change) (*board.Item, error) {
	blocked := c.BlockedBy != ""
	reason := ""
	if blocked {
		reason = "blocked_by: " + c.BlockedBy
	}
	it := &board.Item{
		PlanID:        s.planID,
		Kind:          kindFor(c),
		Title:         fmt.Sprintf("#%d %s", c.ID, c.Title),
		ColumnKey:     c.ColumnFor(),
		LaneKey:       c.LaneFor(),
		SpecRef:       c.Spec,
		SpecStatus:    c.SpecStatusFor(),
		Priority:      c.PriorityFor(),
		Blocked:       blocked,
		BlockedReason: reason,
		ExtKey:        fmt.Sprintf("docket:%d", c.ID),
		Labels:        c.Labels(),
	}
	return s.st.UpsertByExtKey(ctx, "docket-sync", it)
}

// kindFor: a change that other changes depend on reads as an epic; else a story
// (or bug for fix-type changes).
func kindFor(c Change) board.Kind {
	if c.Type == "fix" {
		return board.KindBug
	}
	return board.KindStory
}
