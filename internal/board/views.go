package board

// The board is organized as three flat views (tabs). Backlog and Done are
// decoupled from the in-flight board: the in-flight board shows only work that is
// actively moving, so it stays focused on "what's in flight and what depends on
// what". Each view assigns an item to a (lane, column) cell by its own rules —
// lanes and columns mean different things per view. No nesting; dependencies are
// shown as links.

// ViewKind identifies one of the three tabs.
type ViewKind string

const (
	ViewBacklog  ViewKind = "backlog"
	ViewInFlight ViewKind = "inflight"
	ViewDone     ViewKind = "done"
)

// ViewAxis is a lane or column definition within a view.
type ViewAxis struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}

// ViewDef describes a view's axes for a renderer.
type ViewDef struct {
	Kind    ViewKind   `json:"kind"`
	Name    string     `json:"name"`
	Lanes   []ViewAxis `json:"lanes"`
	Columns []ViewAxis `json:"columns"`
}

// Views returns the three view definitions. These axes are stable and renderer-
// facing; item placement is computed by PlaceInView.
func Views() []ViewDef {
	return []ViewDef{
		{
			Kind: ViewBacklog, Name: "Backlog",
			// Lanes = grooming readiness: what still needs work before it can be built.
			Lanes: []ViewAxis{
				{Key: "needs_grooming", Name: "Needs Grooming"},
				{Key: "in_spec", Name: "In Spec"},
				{Key: "in_design", Name: "In Design"},
				{Key: "build_ready", Name: "Build-Ready"},
			},
			// Columns = priority.
			Columns: []ViewAxis{
				{Key: "p0", Name: "P0"}, {Key: "p1", Name: "P1"},
				{Key: "p2", Name: "P2"}, {Key: "p3", Name: "P3"},
			},
		},
		{
			Kind: ViewInFlight, Name: "In-Flight",
			// Lanes = parts of the work, so you can see which parts aren't done.
			Lanes: []ViewAxis{
				{Key: "spec", Name: "Spec"},
				{Key: "design", Name: "Design"},
				{Key: "build", Name: "Build"},
				{Key: "test", Name: "Test"},
				{Key: "review", Name: "Review"},
			},
			// Columns = progress of that part.
			Columns: []ViewAxis{
				{Key: "todo", Name: "To Do"},
				{Key: "doing", Name: "Doing"},
				{Key: "done", Name: "Done"},
			},
		},
		{Kind: ViewDone, Name: "Done"}, // flat list; no lanes/columns
	}
}

// Placement is where an item lands in a view, or Hidden if it doesn't belong.
type Placement struct {
	Lane   string
	Column string
	Hidden bool
}

// InFlightColumns are the underlying item columns considered "actively in flight".
// Backlog/done/deferred/killed are NOT in flight.
var inFlightColumns = map[string]bool{
	"specifying": true, "specd": true, "in_progress": true, "review": true,
}

// PlaceInView maps an item onto a view's (lane, column) grid. This is the single
// place the view taxonomy is applied, so the SPA and any renderer agree.
func PlaceInView(it Item, view ViewKind) Placement {
	switch view {
	case ViewBacklog:
		// Backlog = not yet in flight and not closed. Lane by grooming readiness.
		if inFlightColumns[it.ColumnKey] || isClosedColumn(it.ColumnKey) {
			return Placement{Hidden: true}
		}
		return Placement{Lane: groomingLane(it), Column: priorityColumn(it.Priority)}

	case ViewInFlight:
		if !inFlightColumns[it.ColumnKey] {
			return Placement{Hidden: true}
		}
		lane, col := inflightLaneCol(it)
		return Placement{Lane: lane, Column: col}

	case ViewDone:
		if !isClosedColumn(it.ColumnKey) {
			return Placement{Hidden: true}
		}
		return Placement{Lane: "done", Column: it.ColumnKey}
	}
	return Placement{Hidden: true}
}

func isClosedColumn(col string) bool {
	switch col {
	case "done", "killed", "deferred":
		return true
	}
	return false
}

// groomingLane derives backlog readiness from spec status.
func groomingLane(it Item) string {
	switch it.SpecStatus {
	case SpecApproved:
		return "build_ready"
	case SpecDraft:
		return "in_spec"
	default:
		return "needs_grooming"
	}
}

func priorityColumn(p string) string {
	if p == "" {
		return "p2"
	}
	return p
}

// inflightLaneCol maps an in-flight item to a (part, progress) cell. The part is
// the furthest-along stage the item has entered; progress reflects its column.
func inflightLaneCol(it Item) (lane, col string) {
	switch it.ColumnKey {
	case "specifying":
		lane = "spec"
		col = "doing"
	case "specd":
		lane = "spec"
		col = "done"
	case "in_progress":
		lane = "build"
		col = "doing"
	case "review":
		lane = "review"
		col = "doing"
	default:
		lane = "spec"
		col = "todo"
	}
	if it.Blocked {
		col = "todo" // blocked work is not actively "doing"
	}
	return lane, col
}

// Link is a first-class dependency between two items.
type Link struct {
	From string `json:"from"` // item id
	To   string `json:"to"`   // item id
	Kind string `json:"kind"` // depends_on | related | discovered_from
}
