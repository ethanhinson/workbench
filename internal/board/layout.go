package board

import "fmt"

// Layout is a board's agent-authored UI definition — the renderer draws whatever
// the board declares here, rather than a hard-coded set of tabs. A board with no
// layout (the zero value) renders an empty state until a methodology skill (or the
// board_set_layout tool) sets one. This is the seam that makes the UI "agentic":
// the layout is data, authored per board, and placement is driven by item labels
// (view:/lane:/column:), not by any Go logic.
type Layout struct {
	// Nav is the tab/menu strip, left→right. Each entry opens a named view.
	Nav []NavItem `json:"nav"`
	// Views maps a view id to its definition. Every NavItem.View must key into this.
	Views map[string]LayoutView `json:"views"`
}

// NavItem is one entry in the nav strip.
type NavItem struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	View  string `json:"view"` // key into Layout.Views
}

// ViewType is how a view is drawn.
type ViewType string

const (
	ViewList  ViewType = "list"  // flat scrollable list of items
	ViewLanes ViewType = "lanes" // columns × swimlanes grid
	ViewBoard ViewType = "board" // vertical swimlanes only (no column axis)
	ViewDoc   ViewType = "doc"   // rendered-markdown reader over items' content
	ViewFeed  ViewType = "feed"  // reverse-chronological activity stream (time-series)
)


// LayoutView defines one view. Placement is column-driven: Columns lists the real
// profile column_keys this view OWNS, and an item's nav view is whichever view
// owns its column_key. Within a lanes/board view, cards swimlane by their owned
// column_key by default (so a docket "In Flight" view owning {in_progress,review}
// shows those two lanes for free); an optional Lanes axis buckets by lane_key
// instead for a true 2-D grid. There are no view:/lane:/column: placement labels.
type LayoutView struct {
	Type ViewType `json:"type"`
	// Columns are the real profile column_keys this view owns. Required for every
	// placement view; also the default swimlane axis for lanes/board.
	Columns []LayoutAxis `json:"columns,omitempty"`
	// Lanes is an OPTIONAL secondary axis (bucketed by item.lane_key). When empty, a
	// lanes/board view swimlanes by its owned Columns.
	Lanes []LayoutAxis `json:"lanes,omitempty"`
	// GroupBy is the label namespace whose value colors/labels each card as an
	// epic/group chip (default "group"). Grouping is a glanceable chip, not an axis.
	GroupBy string `json:"group_by,omitempty"`
	Sort    string `json:"sort,omitempty"` // renderer hint: priority | updated_desc | created | title
}

// LayoutAxis is a lane or column: a stable key plus a display label.
type LayoutAxis struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

// ownsColumns reports whether a view type places work items by owning columns.
// feed (activity time-series) and doc (markdown reader) are content views that
// don't bucket by column, so they may omit Columns.
func (t ViewType) ownsColumns() bool {
	return t == ViewList || t == ViewLanes || t == ViewBoard
}

// Validate checks a layout is structurally renderable: nav entries point at real
// views, view types are known, placement views declare owned columns, and axes are
// unique. Column-key validity against the profile is a separate, profile-aware
// check (ValidateForProfile).
func (lo Layout) Validate() error {
	if len(lo.Nav) == 0 {
		return fmt.Errorf("layout needs at least one nav item")
	}
	seenNav := map[string]bool{}
	for i, n := range lo.Nav {
		if n.ID == "" {
			return fmt.Errorf("nav[%d]: id is required", i)
		}
		if seenNav[n.ID] {
			return fmt.Errorf("nav id %q is duplicated", n.ID)
		}
		seenNav[n.ID] = true
		if _, ok := lo.Views[n.View]; !ok {
			return fmt.Errorf("nav %q references unknown view %q", n.ID, n.View)
		}
	}
	for id, v := range lo.Views {
		switch v.Type {
		case ViewList, ViewLanes, ViewBoard, ViewDoc, ViewFeed:
		default:
			return fmt.Errorf("view %q: unknown type %q (want list|lanes|board|doc|feed)", id, v.Type)
		}
		if v.Type.ownsColumns() && len(v.Columns) == 0 {
			return fmt.Errorf("view %q (%s) needs at least one owned column", id, v.Type)
		}
		if err := axesUnique(v.Columns, "column", id); err != nil {
			return err
		}
		if err := axesUnique(v.Lanes, "lane", id); err != nil {
			return err
		}
	}
	return nil
}

// ValidateForProfile checks the layout's column ownership against the board's real
// profile columns: every owned column key must be a real column, and no column may
// be owned by two views (which would make nav routing ambiguous, since an item's
// view is derived from its column_key). An unowned column is NOT an error — its
// items fall to the renderer's unfiled fallback — but is returned in `unowned` so a
// caller can surface it as a warning.
func (lo Layout) ValidateForProfile(cols []ColumnDef) (unowned []string, err error) {
	real := map[string]bool{}
	for _, c := range cols {
		real[c.Key] = true
	}
	owner := map[string]string{} // column_key -> view id that owns it
	for id, v := range lo.Views {
		if !v.Type.ownsColumns() {
			continue
		}
		for _, c := range v.Columns {
			if !real[c.Key] {
				return nil, fmt.Errorf("view %q owns column %q, which is not a profile column", id, c.Key)
			}
			if prev, ok := owner[c.Key]; ok {
				return nil, fmt.Errorf("column %q is owned by both view %q and view %q; a column may belong to at most one view", c.Key, prev, id)
			}
			owner[c.Key] = id
		}
	}
	for _, c := range cols {
		if _, ok := owner[c.Key]; !ok {
			unowned = append(unowned, c.Key)
		}
	}
	return unowned, nil
}

func axesUnique(axes []LayoutAxis, kind, viewID string) error {
	seen := map[string]bool{}
	for _, a := range axes {
		if a.Key == "" {
			return fmt.Errorf("view %q: a %s is missing its key", viewID, kind)
		}
		if seen[a.Key] {
			return fmt.Errorf("view %q: duplicate %s key %q", viewID, kind, a.Key)
		}
		seen[a.Key] = true
	}
	return nil
}
