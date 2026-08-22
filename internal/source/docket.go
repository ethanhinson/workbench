package source

import (
	"context"
	"fmt"

	"github.com/ethanhinson/kanban-mcp/internal/board"
	"github.com/ethanhinson/kanban-mcp/internal/docket"
)

// docketProvider projects a docket backlog (markdown-on-a-branch) onto the board.
// It delegates all parsing + board-term mapping to the docket package (which is
// store-free), then translates into the source seam's ExternalItem/ExternalLink.
type docketProvider struct {
	docsDir string
}

func (p *docketProvider) Kind() string { return "docket" }

func (p *docketProvider) Fetch(ctx context.Context) ([]ExternalItem, []ExternalLink, error) {
	changes, err := docket.Import(p.docsDir)
	if err != nil {
		return nil, nil, err
	}
	adrs, err := docket.ImportADRs(p.docsDir)
	if err != nil {
		return nil, nil, err
	}

	var items []ExternalItem
	var links []ExternalLink

	for _, c := range changes {
		blocked := c.BlockedBy != ""
		reason := ""
		if blocked {
			reason = "blocked_by: " + c.BlockedBy
		}
		items = append(items, ExternalItem{
			ExtKey:        changeKey(c.ID),
			Kind:          kindFor(c),
			Title:         fmt.Sprintf("#%d %s", c.ID, c.Title),
			Column:        c.ColumnFor(),
			Lane:          c.LaneFor(),
			SpecRef:       c.Spec,
			SpecStatus:    c.SpecStatusFor(),
			Priority:      c.PriorityFor(),
			Blocked:       blocked,
			BlockedReason: reason,
			Labels:        c.Labels(),
			Section:       sectionFor(c),
		})
		for _, n := range c.DependsOn {
			links = append(links, ExternalLink{FromExtKey: changeKey(c.ID), ToExtKey: changeKey(n), Kind: "depends_on"})
		}
		for _, n := range c.DiscoveredFrom {
			links = append(links, ExternalLink{FromExtKey: changeKey(c.ID), ToExtKey: changeKey(n), Kind: "discovered_from"})
		}
		for _, n := range c.Related {
			links = append(links, ExternalLink{FromExtKey: changeKey(c.ID), ToExtKey: changeKey(n), Kind: "related"})
		}
	}

	// ADRs are reference records: read-only decision cards in their own lane,
	// linked back to the change they came out of via a "related" edge.
	for _, a := range adrs {
		items = append(items, ExternalItem{
			ExtKey:     adrKey(a.ID),
			Kind:       board.KindSpike,
			Title:      fmt.Sprintf("ADR-%d %s", a.ID, a.Title),
			Column:     "done", // decisions are settled records, not in-flight work
			Lane:       "adr",
			SpecRef:    a.Path,
			SpecStatus: board.SpecApproved,
			Priority:   board.PriorityP3,
			Labels: []board.Label{
				{NS: "area", Value: "adr"},
				{NS: "spec", Value: string(board.SpecApproved)},
			},
			Section: SectionADR,
		})
		if a.Change != 0 {
			links = append(links, ExternalLink{FromExtKey: adrKey(a.ID), ToExtKey: changeKey(a.Change), Kind: "related"})
		}
	}

	return items, links, nil
}

func changeKey(id int) string { return fmt.Sprintf("docket:%d", id) }
func adrKey(id int) string    { return fmt.Sprintf("adr:%d", id) }

// kindFor: a fix-type change reads as a bug; everything else a story.
func kindFor(c docket.Change) board.Kind {
	if c.Type == "fix" {
		return board.KindBug
	}
	return board.KindStory
}

// sectionFor tags which decoupled view a change projects into.
func sectionFor(c docket.Change) Section {
	switch c.ColumnFor() {
	case "done", "killed", "deferred":
		return SectionDone
	default:
		return SectionBacklog
	}
}
