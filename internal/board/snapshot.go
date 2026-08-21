package board

// Snapshot is the renderer-agnostic view contract for the board. It is the
// single pluggable seam: the default SPA, an agent-generated component, a TUI,
// or a static export all consume this exact shape. Bump SchemaVersion on any
// breaking change so renderers can negotiate.
type Snapshot struct {
	SchemaVersion int           `json:"schema_version"`
	Plan          SnapshotPlan  `json:"plan"`
	Columns       []ColumnDef   `json:"columns"`
	Lanes         []Lane        `json:"lanes"`
	Items         []Item        `json:"items"`       // flat; renderers nest via parent_id
	Cells         map[string]Cell `json:"cells"`     // "lane|column" -> item ids (precomputed grid)
	Stats         SnapshotStats `json:"stats"`
}

const SnapshotSchemaVersion = 1

// SnapshotPlan carries the methodology binding so a renderer can label the axes
// correctly ("lane = agent" vs "lane = epic" vs "lane = class of service").
type SnapshotPlan struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	ProfileKey    string `json:"profile"`
	LaneDimension string `json:"lane_dimension"`
}

// Cell is one column x lane intersection, holding the ids of the items in it
// (ordered). Renderers look items up in the flat Items slice by id.
type Cell struct {
	Lane    string   `json:"lane"`
	Column  string   `json:"column"`
	ItemIDs []string `json:"item_ids"`
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
func BuildSnapshot(plan Plan, cols []ColumnDef, lanes []Lane, items []Item) Snapshot {
	snap := Snapshot{
		SchemaVersion: SnapshotSchemaVersion,
		Plan: SnapshotPlan{
			ID: plan.ID, Name: plan.Name,
			ProfileKey: plan.ProfileKey, LaneDimension: plan.LaneDimension,
		},
		Columns: cols,
		Lanes:   lanes,
		Items:   items,
		Cells:   map[string]Cell{},
		Stats: SnapshotStats{
			ByColumn: map[string]int{}, ByLane: map[string]int{}, BySpecStatus: map[string]int{},
		},
	}
	for _, it := range items {
		lane := it.LaneKey
		if lane == "" {
			lane = "shared"
		}
		key := lane + "|" + it.ColumnKey
		c := snap.Cells[key]
		c.Lane, c.Column = lane, it.ColumnKey
		c.ItemIDs = append(c.ItemIDs, it.ID)
		snap.Cells[key] = c

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
