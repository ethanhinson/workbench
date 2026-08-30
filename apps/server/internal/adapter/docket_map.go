package adapter

// Placement is the deterministic projection of a docket change onto board axes.
// ColumnKey is the profile lifecycle column the renderer buckets by (an item's nav
// view is whichever layout view owns this column). Group is the change type,
// rendered as a colored chip. There are no view/lane placement values — the
// renderer derives the view from ColumnKey and swimlanes by it.
type Placement struct {
	ColumnKey string // backlog|specifying|specd|in_progress|review|done|deferred|killed
	Group     string // the change type (feat/fix/chore/…) → chip
	Blocked   bool
	Priority  string // p0..p3
}

// Docket lifecycle columns — must match the docket profile's column keys
// (internal/board/profile.go). UpsertByExtKey rejects an unknown column, so these
// are the single source of truth the mapping may emit.
const (
	ColBacklog    = "backlog"     // proposed, needs a spec
	ColSpecifying = "specifying"  // spec drafted, planning
	ColSpecd      = "specd"       // build-ready (spec+plan, or trivial)
	ColInProgress = "in_progress" // has a branch, building
	ColReview     = "review"      // PR open
	ColDone       = "done"        // merged/archived
	ColDeferred   = "deferred"    // parked
	ColKilled     = "killed"      // abandoned
)

// PlaceChange maps a parsed docket change to its board placement. It is a pure,
// total function — every field combination yields a defined result — so it is
// exhaustively unit-testable and can never drift from the board. The case order is
// load-bearing: first match wins, and terminal states, then in-flight signals
// (pr/branch), then the proposed-lane gradient, then the default.
func PlaceChange(ch Change) Placement {
	p := Placement{
		Group:    normalizeType(ch.Type),
		Blocked:  ch.BlockedBy != "",
		Priority: translatePriority(ch.Priority),
	}
	p.ColumnKey = placeColumn(ch)
	return p
}

func placeColumn(ch Change) string {
	switch ch.Status {
	case "done":
		return ColDone
	case "killed":
		return ColKilled
	case "deferred":
		return ColDeferred
	}
	// In-flight signals win over the proposed gradient: an open PR is in review, a
	// cut branch is building. PR beats branch (a change with a PR is in review even
	// if it still has a branch).
	if ch.PR.Set() {
		return ColReview
	}
	if ch.Branch != "" {
		return ColInProgress
	}
	if ch.Status == "in_progress" {
		return ColInProgress
	}
	// Proposed gradient. Trivial changes are build-ready without a spec (the
	// manifest body IS the spec), so this arm precedes the spec/plan checks.
	if ch.Trivial {
		return ColSpecd
	}
	if ch.Spec != "" && ch.Plan != "" {
		return ColSpecd
	}
	if ch.Spec != "" {
		return ColSpecifying
	}
	return ColBacklog
}

// translatePriority maps docket's low|medium|high onto the board's closed p0..p3
// enum (ValidateLabel rejects anything else). Unknown/empty defaults to p2.
func translatePriority(p string) string {
	switch p {
	case "high":
		return "p1"
	case "medium":
		return "p2"
	case "low":
		return "p3"
	default:
		return "p2"
	}
}

// normalizeType passes the change type through as the group chip value; an empty
// type becomes "change" so every card carries a chip.
func normalizeType(t string) string {
	if t == "" {
		return "change"
	}
	return t
}
