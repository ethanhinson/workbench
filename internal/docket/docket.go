// Package docket imports a docket backlog into the kanban board. Docket stores
// each unit of work as a markdown "change" manifest with YAML-ish frontmatter,
// living on the repo's metadata branch under docs/changes/{active,archive}. That
// markdown-on-a-branch shape is a harness-agnostic interface: this importer reads
// the files directly, so it works regardless of which agent/harness drives docket.
package docket

import (
	"bufio"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/ethanhinson/kanban-mcp/internal/board"
)

// Change is the subset of a docket manifest we map onto the board.
type Change struct {
	ID             int
	Slug           string
	Title          string
	Status         string // proposed | deferred | done | killed
	Priority       string // low | medium | high
	Type           string // feat | fix | chore | ...
	Spec           string // path (empty => no spec yet)
	Plan           string // path
	Results        string
	Branch         string
	PR             string
	BlockedBy      string
	Trivial        bool
	DependsOn      []int
	DiscoveredFrom []int
	ADRs           []int
	Path           string
}

// BuildReady is docket's readiness rule as observed on the board: a change is
// build-ready once it has both a spec and a plan; otherwise it needs brainstorm.
func (c Change) BuildReady() bool { return c.Spec != "" && c.Plan != "" }

// Import reads all change manifests under a docket docs directory (the checked-out
// metadata branch, e.g. <repo>/.docket/docs). Returns changes sorted by id.
func Import(docsDir string) ([]Change, error) {
	var changes []Change
	for _, sub := range []string{"changes/active", "changes/archive"} {
		dir := filepath.Join(docsDir, sub)
		entries, err := os.ReadDir(dir)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			c, ok, err := parseChange(filepath.Join(dir, e.Name()))
			if err != nil {
				return nil, err
			}
			if ok {
				changes = append(changes, c)
			}
		}
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i].ID < changes[j].ID })
	return changes, nil
}

// parseChange extracts the frontmatter block (between the first two --- lines).
func parseChange(path string) (Change, bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return Change{}, false, err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	if !sc.Scan() || strings.TrimSpace(sc.Text()) != "---" {
		return Change{}, false, nil // not a frontmatter file
	}
	fields := map[string]string{}
	for sc.Scan() {
		line := sc.Text()
		if strings.TrimSpace(line) == "---" {
			break
		}
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		fields[strings.TrimSpace(k)] = strings.TrimSpace(v)
	}

	c := Change{
		Slug:           fields["slug"],
		Title:          fields["title"],
		Status:         fields["status"],
		Priority:       fields["priority"],
		Type:           fields["type"],
		Spec:           fields["spec"],
		Plan:           fields["plan"],
		Results:        fields["results"],
		Branch:         fields["branch"],
		PR:             fields["pr"],
		BlockedBy:      fields["blocked_by"],
		Trivial:        fields["trivial"] == "true",
		DependsOn:      parseIntList(fields["depends_on"]),
		DiscoveredFrom: parseIntList(fields["discovered_from"]),
		ADRs:           parseIntList(fields["adrs"]),
		Path:           path,
	}
	c.ID, _ = strconv.Atoi(fields["id"])
	if c.ID == 0 && c.Title == "" {
		return Change{}, false, nil
	}
	return c, true, nil
}

// parseIntList parses a "[1, 2, 3]" YAML-flow list into ints.
func parseIntList(s string) []int {
	s = strings.TrimSpace(strings.Trim(strings.TrimSpace(s), "[]"))
	if s == "" {
		return nil
	}
	var out []int
	for _, p := range strings.Split(s, ",") {
		if n, err := strconv.Atoi(strings.TrimSpace(p)); err == nil {
			out = append(out, n)
		}
	}
	return out
}

// --- mapping to the kanban board ---

// ColumnFor maps a change's docket status+readiness to a docket-profile column.
func (c Change) ColumnFor() string {
	switch c.Status {
	case "done":
		return "done"
	case "killed":
		return "killed"
	case "deferred":
		return "deferred"
	case "proposed":
		switch {
		case c.PR != "":
			return "review"
		case c.Branch != "":
			return "in_progress"
		case c.BuildReady():
			return "specd"
		default:
			return "backlog"
		}
	}
	return "backlog"
}

// PriorityFor maps docket priority to the kanban p0..p3 taxonomy.
func (c Change) PriorityFor() string {
	switch c.Priority {
	case "high":
		return board.PriorityP0
	case "medium":
		return board.PriorityP1
	case "low":
		return board.PriorityP3
	}
	return board.PriorityP2
}

// SpecStatusFor maps whether a spec/plan exists to the SDD spec status.
func (c Change) SpecStatusFor() board.SpecStatus {
	switch {
	case c.Results != "" || c.Status == "done":
		return board.SpecApproved
	case c.Spec != "":
		return board.SpecDraft
	default:
		return board.SpecMissing
	}
}

// LaneFor is the change's swim lane under the docket profile (lane = type).
func (c Change) LaneFor() string {
	if c.Type == "" {
		return "shared"
	}
	return c.Type
}

// Labels returns the consistent, namespaced label set for a change.
func (c Change) Labels() []board.Label {
	labels := []board.Label{
		{NS: "priority", Value: c.PriorityFor()},
		{NS: "spec", Value: string(c.SpecStatusFor())},
	}
	if c.Type != "" {
		labels = append(labels, board.Label{NS: "area", Value: c.Type})
	}
	if c.BuildReady() {
		labels = append(labels, board.Label{NS: "area", Value: "build-ready"})
	} else if c.Status == "proposed" {
		labels = append(labels, board.Label{NS: "area", Value: "needs-brainstorm"})
	}
	return labels
}
