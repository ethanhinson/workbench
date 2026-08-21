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

	// Second pass: upsert all items (without parents) so ext keys exist...
	for _, c := range changes {
		if _, err := s.upsert(ctx, c, ""); err != nil {
			return Result{}, err
		}
	}
	// Third pass: set parent links now that all cards exist.
	for _, c := range changes {
		parentExt := parentOf(c, changes)
		if parentExt == "" {
			continue
		}
		if _, ok := s.st.ItemIDByExtKey(ctx, s.planID, parentExt); !ok {
			continue
		}
		if _, err := s.upsert(ctx, c, parentExt); err != nil {
			return Result{}, err
		}
	}

	return Result{Changes: len(changes), Lanes: laneCount}, nil
}

func (s *Syncer) upsert(ctx context.Context, c Change, parentExt string) (*board.Item, error) {
	var parentID string
	if parentExt != "" {
		if id, ok := s.st.ItemIDByExtKey(ctx, s.planID, parentExt); ok {
			parentID = id
		}
	}
	blocked := c.BlockedBy != ""
	reason := ""
	if blocked {
		reason = "blocked_by: " + c.BlockedBy
	}
	it := &board.Item{
		PlanID:        s.planID,
		ParentID:      parentID,
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

// parentOf returns the ext-key of a change's parent for nesting: the single
// docket change it was discovered_from (preferred) or depends_on, if that parent
// is itself in the imported set.
func parentOf(c Change, all []Change) string {
	inSet := map[int]bool{}
	for _, x := range all {
		inSet[x.ID] = true
	}
	cands := c.DiscoveredFrom
	if len(cands) == 0 {
		cands = c.DependsOn
	}
	if len(cands) == 1 && inSet[cands[0]] && cands[0] != c.ID {
		return fmt.Sprintf("docket:%d", cands[0])
	}
	return ""
}
