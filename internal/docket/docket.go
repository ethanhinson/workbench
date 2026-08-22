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
	Related        []int
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

// ADR is the subset of a docket ADR (architecture decision record) we surface on
// the board as a reference card. ADRs live under docs/adrs as frontmatter markdown
// and link back to the change that introduced them.
type ADR struct {
	ID       int
	Slug     string
	Title    string
	Status   string // Accepted | Superseded | Reversed | ...
	Date     string
	Change   int   // the change this decision came out of (0 = none)
	RelatesTo []int
	Path     string
}

// ImportADRs reads all ADR manifests under <docsDir>/adrs. Returns them sorted by
// id. A missing adrs dir is not an error (returns nil).
func ImportADRs(docsDir string) ([]ADR, error) {
	dir := filepath.Join(docsDir, "adrs")
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var adrs []ADR
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		a, ok, err := parseADR(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		if ok {
			adrs = append(adrs, a)
		}
	}
	sort.Slice(adrs, func(i, j int) bool { return adrs[i].ID < adrs[j].ID })
	return adrs, nil
}

func parseADR(path string) (ADR, bool, error) {
	fields, ok, err := frontmatter(path)
	if err != nil || !ok {
		return ADR{}, false, err
	}
	a := ADR{
		Slug:      fields["slug"],
		Title:     unquote(fields["title"]),
		Status:    fields["status"],
		Date:      fields["date"],
		RelatesTo: parseIntList(fields["relates_to"]),
		Path:      path,
	}
	a.ID, _ = strconv.Atoi(fields["id"])
	a.Change, _ = strconv.Atoi(fields["change"])
	if a.ID == 0 && a.Title == "" {
		return ADR{}, false, nil
	}
	return a, true, nil
}

// unquote strips surrounding double quotes from a YAML scalar.
func unquote(s string) string {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}
	return s
}

// parseChange extracts the frontmatter block (between the first two --- lines).
// frontmatter reads the YAML-ish frontmatter block (between the first two ---
// lines) of a markdown file into a flat key->value map. ok is false when the file
// doesn't open with a --- fence (not a frontmatter doc).
func frontmatter(path string) (map[string]string, bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	if !sc.Scan() || strings.TrimSpace(sc.Text()) != "---" {
		return nil, false, nil // not a frontmatter file
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
	return fields, true, nil
}

func parseChange(path string) (Change, bool, error) {
	fields, ok, err := frontmatter(path)
	if err != nil || !ok {
		return Change{}, false, err
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
		Related:        parseIntList(fields["related"]),
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
		case c.Spec != "": // spec drafted but not yet build-ready (no plan)
			return "specifying"
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
