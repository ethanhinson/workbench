// Package board defines the domain model and the SDD-opinionated defaults:
// the workflow columns, the swim-lane conventions, and the consistent,
// enum-validated label taxonomy that keeps agent-written boards from drifting.
package board

import "fmt"

// Kind is the nesting level of a work item: epic > story > task.
type Kind string

const (
	KindEpic  Kind = "epic"
	KindStory Kind = "story"
	KindTask  Kind = "task"
	KindBug   Kind = "bug"
	KindSpike Kind = "spike"
)

func (k Kind) Valid() bool {
	switch k {
	case KindEpic, KindStory, KindTask, KindBug, KindSpike:
		return true
	}
	return false
}

// Plan is the top-level shared board. One SQLite file holds exactly one Plan.
type Plan struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Description   string `json:"description,omitempty"`
	ProfileKey    string `json:"profile"`        // active methodology profile
	LaneDimension string `json:"lane_dimension"` // what a swim lane means under it
	PoliciesJSON  string `json:"-"`              // persisted enforcement rules (JSON)
	CreatedAt     string `json:"created_at"`
	UpdatedAt     string `json:"updated_at"`
}

// ColumnDef is a workflow stage (a kanban column).
type ColumnDef struct {
	Key      string `json:"key"`
	Name     string `json:"name"`
	Position int    `json:"position"`
	IsDone   bool   `json:"is_done"`
	WIPLimit *int   `json:"wip_limit,omitempty"`
}

// DefaultColumns is the SDD-opinionated workflow. Overridable per plan.
// Blocked is a flag on an item, not a column, so a blocked item keeps its stage.
func DefaultColumns() []ColumnDef {
	return []ColumnDef{
		{Key: "backlog", Name: "Backlog", Position: 0},
		{Key: "specifying", Name: "Specifying", Position: 1},
		{Key: "specd", Name: "Spec'd", Position: 2},
		{Key: "in_progress", Name: "In Progress", Position: 3},
		{Key: "review", Name: "Review", Position: 4},
		{Key: "done", Name: "Done", Position: 5, IsDone: true},
	}
}

// SpecStatus tracks the spec-driven-development state of an item.
type SpecStatus string

const (
	SpecMissing  SpecStatus = "missing"
	SpecDraft    SpecStatus = "draft"
	SpecApproved SpecStatus = "approved"
)

func (s SpecStatus) Valid() bool {
	switch s {
	case SpecMissing, SpecDraft, SpecApproved:
		return true
	}
	return false
}

// Priority buckets.
const (
	PriorityP0 = "p0"
	PriorityP1 = "p1"
	PriorityP2 = "p2"
	PriorityP3 = "p3"
)

func ValidPriority(p string) bool {
	switch p {
	case PriorityP0, PriorityP1, PriorityP2, PriorityP3:
		return true
	}
	return false
}

// Label taxonomy. Labels are namespaced ("ns:value") and enum-validated, except
// for open namespaces (agent, area) where any slug value is allowed.
// This is what delivers "labels should be very consistent".
var labelEnums = map[string]map[string]bool{
	"type":     {"epic": true, "story": true, "task": true, "bug": true, "spike": true},
	"priority": {"p0": true, "p1": true, "p2": true, "p3": true},
	"spec":     {"missing": true, "draft": true, "approved": true},
	"stage":    {}, // populated from the plan's columns at validation time
}

// openNamespaces accept any non-empty slug value.
var openNamespaces = map[string]bool{
	"agent": true,
	"area":  true,
}

// Label is a single namespaced label.
type Label struct {
	NS    string `json:"ns"`
	Value string `json:"value"`
}

func (l Label) String() string { return l.NS + ":" + l.Value }

// ValidateLabel enforces the taxonomy. stageKeys is the set of valid column keys
// for the plan (so stage: labels stay in sync with the actual columns).
func ValidateLabel(l Label, stageKeys map[string]bool) error {
	if l.NS == "" || l.Value == "" {
		return fmt.Errorf("label needs both namespace and value (got %q:%q)", l.NS, l.Value)
	}
	if openNamespaces[l.NS] {
		return nil
	}
	if l.NS == "stage" {
		if !stageKeys[l.Value] {
			return fmt.Errorf("stage label %q is not a column in this plan", l.Value)
		}
		return nil
	}
	enum, known := labelEnums[l.NS]
	if !known {
		return fmt.Errorf("unknown label namespace %q (allowed: type, priority, spec, stage, agent, area)", l.NS)
	}
	if !enum[l.Value] {
		return fmt.Errorf("invalid %s label %q", l.NS, l.Value)
	}
	return nil
}

// Item is a work item in the nested hierarchy.
type Item struct {
	ID            string     `json:"id"`
	PlanID        string     `json:"plan_id"`
	ParentID      string     `json:"parent_id,omitempty"`
	Kind          Kind       `json:"kind"`
	Title         string     `json:"title"`
	Body          string     `json:"body,omitempty"`
	ColumnKey     string     `json:"column_key"`
	LaneKey       string     `json:"lane_key,omitempty"`
	SpecRef       string     `json:"spec_ref,omitempty"`
	SpecStatus    SpecStatus `json:"spec_status"`
	Priority      string     `json:"priority"`
	Blocked       bool       `json:"blocked"`
	BlockedReason string     `json:"blocked_reason,omitempty"`
	Position      int        `json:"position"`
	Labels        []Label    `json:"labels,omitempty"`
	CreatedAt     string     `json:"created_at"`
	UpdatedAt     string     `json:"updated_at"`
}

// Lane is a configurable swim lane; default one per agent.
type Lane struct {
	Key      string `json:"key"`
	Name     string `json:"name"`
	AgentID  string `json:"agent_id,omitempty"`
	Position int    `json:"position"`
}
