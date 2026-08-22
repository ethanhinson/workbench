package board

// Snapshot is the renderer-agnostic view contract for the board. It is the
// single pluggable seam: the default SPA, an agent-generated component, a TUI,
// or a static export all consume this exact shape. Bump SchemaVersion on any
// breaking change so renderers can negotiate.
//
// The board is presented as three flat views (Backlog / In-Flight / Done). The
// snapshot ships the raw items + links + view definitions; each item carries its
// per-view placement so a renderer can lay out any tab without recomputing the
// taxonomy. Dependencies are shown as links, never as nesting.
type Snapshot struct {
	SchemaVersion int           `json:"schema_version"`
	Plan          SnapshotPlan  `json:"plan"`
	Columns       []ColumnDef   `json:"columns"` // underlying item columns (docket lifecycle)
	Lanes         []Lane        `json:"lanes"`
	Items         []Item        `json:"items"` // flat; each carries its per-view placement
	Links         []Link        `json:"links"`
	Views         []ViewDef     `json:"views"`
	Stats         SnapshotStats `json:"stats"`
}

const SnapshotSchemaVersion = 3

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
// the item, its rendered spec/plan content, and its dependencies both ways.
type ItemDetail struct {
	Item        Item       `json:"item"`
	SpecContent string     `json:"spec_content,omitempty"`
	PlanContent string     `json:"plan_content,omitempty"`
	DependsOn   []LinkedRef `json:"depends_on,omitempty"`   // items this depends on
	DependedBy  []LinkedRef `json:"depended_by,omitempty"`  // items depending on this
	Related     []LinkedRef `json:"related,omitempty"`
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
	TotalItems   int            `json:"total_items"`
	Blocked      int            `json:"blocked"`
	ByColumn     map[string]int `json:"by_column"`
	ByLane       map[string]int `json:"by_lane"`
	BySpecStatus map[string]int `json:"by_spec_status"`
}

// BuildSnapshot assembles a Snapshot from already-loaded board data. Keeping it a
// pure function (no store/db) means any caller — HTTP API, MCP tool, tests — can
// produce the identical contract.
func BuildSnapshot(plan Plan, cols []ColumnDef, lanes []Lane, items []Item, links []Link) Snapshot {
	snap := Snapshot{
		SchemaVersion: SnapshotSchemaVersion,
		Plan: SnapshotPlan{
			ID: plan.ID, Name: plan.Name, Project: plan.Project,
			ProfileKey: plan.ProfileKey, LaneDimension: plan.LaneDimension,
		},
		Columns: cols,
		Lanes:   lanes,
		Items:   items,
		Links:   links,
		Views:   Views(),
		Stats: SnapshotStats{
			ByColumn: map[string]int{}, ByLane: map[string]int{}, BySpecStatus: map[string]int{},
		},
	}
	// Compute each item's placement in every view (flat, no nesting).
	viewKinds := []ViewKind{ViewBacklog, ViewInFlight, ViewDone}
	for i := range snap.Items {
		places := map[string]ItemPlacement{}
		for _, vk := range viewKinds {
			p := PlaceInView(snap.Items[i], vk)
			if !p.Hidden {
				places[string(vk)] = ItemPlacement{Lane: p.Lane, Column: p.Column}
			}
		}
		if len(places) > 0 {
			snap.Items[i].Views = places
		}
	}
	for _, it := range items {
		lane := it.LaneKey
		if lane == "" {
			lane = "shared"
		}
		snap.Stats.TotalItems++
		snap.Stats.ByColumn[it.ColumnKey]++
		snap.Stats.ByLane[lane]++
		snap.Stats.BySpecStatus[string(it.SpecStatus)]++
		if it.Blocked {
			snap.Stats.Blocked++
		}
	}
	return snap
}
