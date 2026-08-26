package board

// Snapshot is the renderer-agnostic view contract for the board. It is the
// single pluggable seam: the default SPA, an agent-generated component, a TUI,
// or a static export all consume this exact shape. Bump SchemaVersion on any
// breaking change so renderers can negotiate.
//
// The board's shape is agent-authored: Layout declares the nav + views, and each
// item's view:/lane:/column: labels place it. A renderer reads Layout, buckets
// Items by their tags, and renders each item's Content for doc views. It never
// reads the filesystem. HasLayout is false when no layout is set (render empty).
// Dependencies are shown as Links, never as nesting.
type Snapshot struct {
	SchemaVersion int           `json:"schema_version"`
	Plan          SnapshotPlan  `json:"plan"`
	Layout        Layout        `json:"layout"`
	HasLayout     bool          `json:"has_layout"`
	Columns       []ColumnDef   `json:"columns"` // underlying item columns (profile lifecycle)
	Lanes         []Lane        `json:"lanes"`
	Items         []Item        `json:"items"` // flat; placement is via each item's column_key/lane_key
	Links         []Link        `json:"links"`
	// Activity is the passive harness event log (a session's tool-call feed),
	// newest-first. It is NOT board work — it lives off the item table entirely, so
	// it can never leak into a work view. A feed view renders it; everything else
	// ignores it.
	Activity []ActivityEntry `json:"activity"`
	Stats    SnapshotStats   `json:"stats"`
}

// ActivityEntry is one harness activity event surfaced for a feed view. It is a
// pure time-series row read from the event log, carrying no placement — a feed is
// ordered by time, not bucketed into lanes.
type ActivityEntry struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`             // the LCD event kind (tool_use_complete, subagent_start, …)
	Category  string `json:"category"`         // tool | subagent | session
	Title     string `json:"title"`            // "Bash — go test ./..."
	Session   string `json:"session,omitempty"`
	Harness   string `json:"harness,omitempty"`
	Status    string `json:"status,omitempty"` // ok | error | ""
	CreatedAt string `json:"created_at"`
}

const SnapshotSchemaVersion = 6

// SnapshotPlan carries the methodology binding so a renderer can label the axes
// correctly ("lane = agent" vs "lane = epic" vs "lane = class of service").
type SnapshotPlan struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Project       string `json:"project,omitempty"`
	ProfileKey    string `json:"profile"`
	LaneDimension string `json:"lane_dimension"`
}

// ItemDetail is the full-detail payload for one card (the click-through view):
// the item (with its content) and its dependencies both ways. Content lives on the
// item itself — no server-side file resolution.
type ItemDetail struct {
	Item       Item        `json:"item"`
	DependsOn  []LinkedRef `json:"depends_on,omitempty"`  // items this depends on
	DependedBy []LinkedRef `json:"depended_by,omitempty"` // items depending on this
	Related    []LinkedRef `json:"related,omitempty"`
}

// LinkedRef is a lightweight reference to another card, for dependency lists.
type LinkedRef struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Column string `json:"column"`
	ExtKey string `json:"ext_key,omitempty"`
}

// SnapshotStats are cheap roll-ups a renderer can show without recomputing.
type SnapshotStats struct {
	TotalItems int `json:"total_items"`
	Blocked    int `json:"blocked"`
}

// BuildSnapshot assembles a Snapshot from already-loaded board data. Keeping it a
// pure function (no store/db) means any caller — HTTP API, MCP tool, tests — can
// produce the identical contract. layout/hasLayout come from the board; placement
// is carried by the items' own labels, so there's no placement computation here.
func BuildSnapshot(plan Plan, layout Layout, hasLayout bool, cols []ColumnDef, lanes []Lane, items []Item, links []Link, activity []ActivityEntry) Snapshot {
	// Keep the layout's maps/slices non-nil so the JSON contract is always an
	// object/array (never null), even for a board with no layout set.
	if layout.Views == nil {
		layout.Views = map[string]LayoutView{}
	}
	if layout.Nav == nil {
		layout.Nav = []NavItem{}
	}
	snap := Snapshot{
		SchemaVersion: SnapshotSchemaVersion,
		Plan: SnapshotPlan{
			ID: plan.ID, Name: plan.Name, Project: plan.Project,
			ProfileKey: plan.ProfileKey, LaneDimension: plan.LaneDimension,
		},
		Layout:    layout,
		HasLayout: hasLayout,
		Columns:   cols,
		Lanes:     lanes,
		Items:     items,
		Links:     links,
		Activity:  activity,
	}
	if snap.Activity == nil {
		snap.Activity = []ActivityEntry{}
	}
	for _, it := range items {
		// Activity-feed events are a session's tool-call log, not board work — keep
		// them out of the work counts the header shows (see Item.IsActivity).
		if it.IsActivity() {
			continue
		}
		snap.Stats.TotalItems++
		if it.Blocked {
			snap.Stats.Blocked++
		}
	}
	return snap
}
