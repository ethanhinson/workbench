package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/ethanhinson/kanban-mcp/internal/board"
)

// Run launches the terminal board against an SSE stream URL (e.g.
// http://localhost:7777/api/stream). It live-updates as the store changes.
func Run(streamURL string) error {
	m := model{streamURL: streamURL, status: "connecting…"}
	p := tea.NewProgram(m, tea.WithAltScreen())
	m.program = p
	go m.consume(context.Background())
	_, err := p.Run()
	return err
}

type snapMsg board.Snapshot
type statusMsg string

type model struct {
	program   *tea.Program
	streamURL string
	snap      board.Snapshot
	status    string
	w, h      int
	laneIdx   int // selected lane for focus (0 = all)
}

// consume streams snapshots and forwards them into the tea program, reconnecting
// on drop. Runs in its own goroutine.
func (m model) consume(ctx context.Context) {
	for {
		err := streamSnapshots(ctx, m.streamURL, func(s board.Snapshot) {
			m.program.Send(snapMsg(s))
		})
		if ctx.Err() != nil {
			return
		}
		_ = err
		m.program.Send(statusMsg("reconnecting…"))
		time.Sleep(1500 * time.Millisecond)
	}
}

func (m model) Init() tea.Cmd { return nil }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
	case snapMsg:
		m.snap = board.Snapshot(msg)
		m.status = "live"
	case statusMsg:
		m.status = string(msg)
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "left", "h":
			if m.laneIdx > 0 {
				m.laneIdx--
			}
		case "right", "l":
			if m.laneIdx < len(m.snap.Lanes) {
				m.laneIdx++
			}
		}
	}
	return m, nil
}

var (
	styHeader = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15"))
	styMuted  = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	styCol    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39")).Padding(0, 1)
	styLane   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("213"))
	styBlock  = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
	styOK     = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	prioColor = map[string]string{"p0": "203", "p1": "215", "p2": "39", "p3": "244"}
)

func (m model) View() string {
	if m.snap.Plan.Name == "" {
		return "\n  " + styMuted.Render(m.status) + "\n"
	}
	s := m.snap
	var b strings.Builder

	// Header line.
	fmt.Fprintf(&b, "%s  %s  %s  %s\n",
		styHeader.Render("kanban-mcp"),
		styMuted.Render(s.Plan.Name),
		styMuted.Render("profile:"+s.Plan.ProfileKey+" · lane="+s.Plan.LaneDimension),
		liveDot(m.status))
	fmt.Fprintf(&b, "%s\n\n", styMuted.Render(fmt.Sprintf(
		"%d items · %d blocked · %s   [←/→ focus lane · q quit]",
		s.Stats.TotalItems, s.Stats.Blocked, specSummary(s.Stats))))

	lanes := s.Lanes
	if m.laneIdx > 0 && m.laneIdx <= len(lanes) {
		lanes = lanes[m.laneIdx-1 : m.laneIdx]
	}

	// Column widths shared across lanes.
	colW := 26
	if m.w > 0 {
		if cw := (m.w - 14) / max(1, len(s.Columns)); cw > 12 {
			colW = min(cw, 32)
		}
	}

	// Column header row.
	b.WriteString(pad("", 12))
	for _, c := range s.Columns {
		b.WriteString(styCol.Render(pad(strings.ToUpper(c.Name), colW-2)))
	}
	b.WriteString("\n")

	items := indexItems(s.Items)
	for _, lane := range lanes {
		b.WriteString(styLane.Render(pad(lane.Name, 12)))
		for _, c := range s.Columns {
			cell := s.Cells[lane.Key+"|"+c.Key]
			b.WriteString(pad(cellSummary(cell, items), colW))
		}
		b.WriteString("\n")
		// second row: top item titles per cell for this lane
		for _, line := range laneItemLines(s, lane.Key, colW) {
			b.WriteString(pad("", 12) + line + "\n")
		}
		b.WriteString("\n")
	}
	return b.String()
}

func indexItems(items []board.Item) map[string]board.Item {
	m := map[string]board.Item{}
	for _, it := range items {
		m[it.ID] = it
	}
	return m
}

func cellSummary(cell board.Cell, items map[string]board.Item) string {
	n := len(cell.ItemIDs)
	if n == 0 {
		return styMuted.Render("·")
	}
	blocked := 0
	for _, id := range cell.ItemIDs {
		if items[id].Blocked {
			blocked++
		}
	}
	s := fmt.Sprintf("%d", n)
	if blocked > 0 {
		s += styBlock.Render(fmt.Sprintf(" ⚑%d", blocked))
	}
	return s
}

// laneItemLines renders up to 3 top-level item titles per column for a lane, as
// aligned columns of short cards.
func laneItemLines(s board.Snapshot, laneKey string, colW int) []string {
	const rows = 3
	byID := indexItems(s.Items)
	// gather top-level items per column
	perCol := make([][]board.Item, len(s.Columns))
	for ci, c := range s.Columns {
		cell := s.Cells[laneKey+"|"+c.Key]
		for _, id := range cell.ItemIDs {
			it := byID[id]
			if it.ParentID == "" {
				perCol[ci] = append(perCol[ci], it)
			}
		}
		sort.SliceStable(perCol[ci], func(a, b int) bool {
			return perCol[ci][a].Priority < perCol[ci][b].Priority
		})
	}
	var lines []string
	for r := 0; r < rows; r++ {
		var line strings.Builder
		any := false
		for ci := range s.Columns {
			cellTxt := ""
			if r < len(perCol[ci]) {
				it := perCol[ci][r]
				any = true
				dot := lipgloss.NewStyle().Foreground(lipgloss.Color(prioColor[it.Priority])).Render("●")
				title := it.Title
				if len(title) > colW-4 {
					title = title[:colW-5] + "…"
				}
				cellTxt = dot + " " + title
			}
			line.WriteString(pad(cellTxt, colW))
		}
		if !any {
			break
		}
		lines = append(lines, line.String())
	}
	return lines
}

func specSummary(st board.SnapshotStats) string {
	parts := []string{}
	for _, k := range []string{"approved", "draft", "missing"} {
		if v := st.BySpecStatus[k]; v > 0 {
			parts = append(parts, fmt.Sprintf("%s:%d", k, v))
		}
	}
	return strings.Join(parts, " ")
}

func liveDot(status string) string {
	if status == "live" {
		return styOK.Render("● live")
	}
	return styMuted.Render("○ " + status)
}

func pad(s string, w int) string {
	l := lipgloss.Width(s)
	if l >= w {
		return s
	}
	return s + strings.Repeat(" ", w-l)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// RenderForTest renders a board snapshot to a styled string at the given width,
// without a live terminal — used for docs/screenshots and golden tests.
func RenderForTest(snap board.Snapshot, width int) string {
	m := model{snap: snap, status: "live", w: width, h: 50}
	return m.View()
}
