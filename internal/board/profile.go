package board

import "fmt"

// A Profile is a methodology binding. It is the single place that gives meaning
// to both board axes and enforces how they interact:
//
//   - columns       define the workflow (the vertical stages)
//   - LaneDimension declares what a swim lane MEANS for this methodology
//   - Policies      are the rules the server enforces on moves/creates — this is
//     where "the spec workflow influences swim lanes" actually lives.
//
// Different methodologies answer the workflow<->lane question differently, so it
// is a per-profile decision rather than a hard-coded one.
type Profile struct {
	Key           string        `json:"key"`
	Name          string        `json:"name"`
	Description   string        `json:"description,omitempty"`
	LaneDimension LaneDimension `json:"lane_dimension"`
	Columns       []ColumnDef   `json:"columns"`
	SeedLanes     []Lane        `json:"seed_lanes,omitempty"` // lanes created up front (class-of-service, etc.)
	Policies      Policies      `json:"policies"`
}

// LaneDimension names what a lane represents under a methodology. It is a free
// string (fully custom per profile) with a few well-known values the auto-lane
// logic understands.
type LaneDimension string

const (
	LaneByAgent          LaneDimension = "agent"            // one lane per connecting agent (SDD default)
	LaneByEpic           LaneDimension = "epic"             // one lane per epic (Scrum / Spec-DD)
	LaneByClassOfService LaneDimension = "class_of_service" // expedite/standard/fixed-date (classic Kanban)
	LaneCustom           LaneDimension = "custom"           // profile/user manages lanes explicitly
)

// Policies is the enforcement bundle. Every field is optional; an empty Policies
// enforces nothing (purely organizational).
type Policies struct {
	// ColumnGates: to LEAVE the keyed column, an item must satisfy the gate.
	// This is the SDD lever — e.g. leaving "specd" requires spec_status=approved.
	// Applies uniformly across every lane (workflow constrains lanes).
	ColumnGates map[string]Gate `json:"column_gates,omitempty"`

	// Transitions: allowed column->column moves. Empty means any move is allowed.
	Transitions map[string][]string `json:"transitions,omitempty"`

	// LaneWIP: max concurrent items per lane (keyed by lane key). This is the
	// lane axis carrying its own capacity independent of column WIP limits.
	LaneWIP map[string]int `json:"lane_wip,omitempty"`

	// ExemptLanes bypass ColumnGates and WIP entirely (e.g. the expedite lane).
	// This is the inverse coupling: a lane influencing the workflow.
	ExemptLanes []string `json:"exempt_lanes,omitempty"`
}

// Gate is a precondition an item must meet to leave a column.
type Gate struct {
	RequireSpecStatus SpecStatus `json:"require_spec_status,omitempty"` // e.g. "approved"
	RequireNotBlocked bool       `json:"require_not_blocked,omitempty"` // can't advance while blocked
}

// CheckLeave validates that item may leave its current column toward `to`,
// applying gates and allowed transitions. Exempt lanes skip all checks.
func (p Profile) CheckLeave(item Item, to string) error {
	if p.isExempt(item.LaneKey) {
		return nil
	}
	if gate, ok := p.Policies.ColumnGates[item.ColumnKey]; ok {
		if gate.RequireSpecStatus != "" && item.SpecStatus != gate.RequireSpecStatus {
			return fmt.Errorf("cannot leave %q: spec_status must be %q (is %q)",
				item.ColumnKey, gate.RequireSpecStatus, item.SpecStatus)
		}
		if gate.RequireNotBlocked && item.Blocked {
			return fmt.Errorf("cannot leave %q while item is blocked", item.ColumnKey)
		}
	}
	if allowed, ok := p.Policies.Transitions[item.ColumnKey]; ok {
		for _, a := range allowed {
			if a == to {
				return nil
			}
		}
		return fmt.Errorf("transition %q -> %q is not allowed by the %s profile",
			item.ColumnKey, to, p.Key)
	}
	return nil
}

// LaneWIPLimit returns the per-lane WIP cap and whether one is set (exempt lanes
// return no limit).
func (p Profile) LaneWIPLimit(laneKey string) (int, bool) {
	if p.isExempt(laneKey) {
		return 0, false
	}
	n, ok := p.Policies.LaneWIP[laneKey]
	return n, ok
}

func (p Profile) isExempt(laneKey string) bool {
	for _, e := range p.Policies.ExemptLanes {
		if e == laneKey {
			return true
		}
	}
	return false
}

// --- Built-in presets (overridable per plan) ---

// Profiles returns the built-in methodology presets keyed by Key.
func Profiles() map[string]Profile {
	presets := []Profile{sddProfile(), scrumProfile(), kanbanProfile(), docketProfile()}
	m := make(map[string]Profile, len(presets))
	for _, p := range presets {
		m[p.Key] = p
	}
	return m
}

// LookupProfile returns a built-in preset by key.
func LookupProfile(key string) (Profile, bool) {
	p, ok := Profiles()[key]
	return p, ok
}

func sddProfile() Profile {
	return Profile{
		Key:           "sdd",
		Name:          "Spec-Driven Development",
		Description:   "Spec lifecycle columns; one lane per agent; a spec must be approved before implementation.",
		LaneDimension: LaneByAgent,
		Columns:       DefaultColumns(),
		Policies: Policies{
			// The workflow influences every lane: nothing leaves Spec'd until the
			// spec is approved, and nothing advances while blocked.
			ColumnGates: map[string]Gate{
				"specd":       {RequireSpecStatus: SpecApproved, RequireNotBlocked: true},
				"in_progress": {RequireNotBlocked: true},
			},
		},
	}
}

func scrumProfile() Profile {
	cols := []ColumnDef{
		{Key: "todo", Name: "To Do", Position: 0},
		{Key: "in_progress", Name: "In Progress", Position: 1},
		{Key: "review", Name: "Review", Position: 2},
		{Key: "done", Name: "Done", Position: 3, IsDone: true},
	}
	return Profile{
		Key:           "scrum",
		Name:          "Scrum",
		Description:   "Sprint board; one lane per epic; strict left-to-right transitions.",
		LaneDimension: LaneByEpic,
		Columns:       cols,
		Policies: Policies{
			// Strict forward flow: the workflow itself is the constraint, applied
			// within each epic's lane.
			Transitions: map[string][]string{
				"todo":        {"in_progress"},
				"in_progress": {"review", "todo"},
				"review":      {"done", "in_progress"},
			},
		},
	}
}

func kanbanProfile() Profile {
	cols := []ColumnDef{
		{Key: "backlog", Name: "Backlog", Position: 0},
		{Key: "in_progress", Name: "In Progress", Position: 1, WIPLimit: intp(3)},
		{Key: "done", Name: "Done", Position: 2, IsDone: true},
	}
	return Profile{
		Key:           "kanban",
		Name:          "Classic Kanban",
		Description:   "Value stream with WIP limits; lanes are classes of service; the expedite lane bypasses limits.",
		LaneDimension: LaneByClassOfService,
		Columns:       cols,
		SeedLanes: []Lane{
			{Key: "expedite", Name: "Expedite", Position: 0},
			{Key: "standard", Name: "Standard", Position: 1},
			{Key: "fixed_date", Name: "Fixed Date", Position: 2},
		},
		Policies: Policies{
			LaneWIP:     map[string]int{"standard": 5},
			ExemptLanes: []string{"expedite"}, // the lane influences the workflow: no limits apply
		},
	}
}

// docketProfile mirrors docket's change lifecycle (a change ~= one PR). Columns
// track docket status x readiness; lanes are the change's type (feat/fix/...),
// which is the natural swim dimension for a docket backlog. Enforcement is light
// because docket itself owns the real workflow; kanban-mcp is the pane of glass.
func docketProfile() Profile {
	cols := []ColumnDef{
		{Key: "backlog", Name: "Backlog", Position: 0},          // proposed + needs-brainstorm
		{Key: "specifying", Name: "Specifying", Position: 1},    // brainstorm in progress (spec draft)
		{Key: "specd", Name: "Build-Ready", Position: 2},        // proposed + build-ready (spec/plan present)
		{Key: "in_progress", Name: "In Progress", Position: 3},  // has a branch/PR, not merged
		{Key: "review", Name: "In Review", Position: 4},         // PR open / implemented
		{Key: "done", Name: "Done", Position: 5, IsDone: true},  // merged/archived
		{Key: "deferred", Name: "Deferred", Position: 6},        // deferred
		{Key: "killed", Name: "Killed", Position: 7, IsDone: true},
	}
	return Profile{
		Key:           "docket",
		Name:          "Docket (imported)",
		Description:   "Maps a docket backlog (change manifests on the metadata branch) into the kanban board.",
		LaneDimension: "type", // swim lane = docket change type (feat/fix/chore/...)
		Columns:       cols,
		Policies:      Policies{}, // no gates: docket is the source of truth for its own rules
	}
}

func intp(n int) *int { return &n }
