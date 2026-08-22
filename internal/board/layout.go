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
)

func (t ViewType) needsLanes() bool   { return t == ViewLanes || t == ViewBoard }
func (t ViewType) needsColumns() bool { return t == ViewLanes }

// LayoutView defines one view. Which of Lanes/Columns are required depends on Type.
// Include selects which items appear (by default the items tagged view:<the view's
// own id>); items are then bucketed into Lanes/Columns by their lane:/column: tags.
type LayoutView struct {
	Type    ViewType    `json:"type"`
	Lanes   []LayoutAxis `json:"lanes,omitempty"`
	Columns []LayoutAxis `json:"columns,omitempty"`
	Include *ViewInclude `json:"include,omitempty"`
	Sort    string       `json:"sort,omitempty"` // renderer hint: priority | updated_desc | created | title
}

// LayoutAxis is a lane or column: a stable key plus a display label.
type LayoutAxis struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

// ViewInclude selects which items a view shows. View is the view: label value an
// item must carry; empty means "the view's own id".
type ViewInclude struct {
	View string `json:"view,omitempty"`
}

// IncludeView returns the view: label value items must carry to appear in the named
// view — the explicit Include.View if set, else the view's own id.
func (l LayoutView) IncludeView(viewID string) string {
	if l.Include != nil && l.Include.View != "" {
		return l.Include.View
	}
	return viewID
}

// Validate checks a layout is renderable: nav entries point at real views, view
// types are known, and lane/column axes are present where the type needs them.
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
		case ViewList, ViewLanes, ViewBoard, ViewDoc:
		default:
			return fmt.Errorf("view %q: unknown type %q (want list|lanes|board|doc)", id, v.Type)
		}
		if v.Type.needsLanes() && len(v.Lanes) == 0 {
			return fmt.Errorf("view %q (%s) needs at least one lane", id, v.Type)
		}
		if v.Type.needsColumns() && len(v.Columns) == 0 {
			return fmt.Errorf("view %q (%s) needs at least one column", id, v.Type)
		}
		if err := axesUnique(v.Lanes, "lane", id); err != nil {
			return err
		}
		if err := axesUnique(v.Columns, "column", id); err != nil {
			return err
		}
	}
	return nil
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
