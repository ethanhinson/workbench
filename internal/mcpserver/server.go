// Package mcpserver exposes the kanban board as MCP tools. Each tool maps to a
// store operation with a typed, JSON-schema'd input so agents get validation for
// free. The design goal is a single pane of glass for SDD / Spec-DD work.
package mcpserver

import (
	"context"
	"fmt"
	"strings"

	"github.com/ethanhinson/kanban-mcp/internal/board"
	"github.com/ethanhinson/kanban-mcp/internal/docket"
	"github.com/ethanhinson/kanban-mcp/internal/store"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Server binds the store and the calling agent's identity to the MCP tools.
type Server struct {
	st      *store.Store
	plan    *board.Plan
	agentID string // this server instance's owning agent; used for lanes + event attribution
}

// PlanID returns the id of the plan this server serves (used to wire the viz layer).
func (s *Server) PlanID() string { return s.plan.ID }

// New builds an MCP server exposing the plan at the given db. agentID identifies
// the agent driving this instance. profileKey selects the methodology (sdd|scrum|
// kanban|...) on first init; it is ignored if the plan already exists.
func New(ctx context.Context, st *store.Store, planName, agentID, profileKey string) (*mcp.Server, *Server, error) {
	plan, err := st.EnsurePlan(ctx, planName, "", profileKey)
	if err != nil {
		return nil, nil, err
	}
	if agentID == "" {
		agentID = "agent"
	}
	// Only auto-create a per-agent lane when the active profile's lane dimension
	// is "agent" — under epic/class-of-service profiles the agent isn't the lane.
	if plan.LaneDimension == string(board.LaneByAgent) {
		if _, err := st.EnsureLane(ctx, plan.ID, board.Lane{
			Key: agentID, Name: agentID, AgentID: agentID,
		}); err != nil {
			return nil, nil, err
		}
	}

	s := &Server{st: st, plan: plan, agentID: agentID}
	srv := mcp.NewServer(&mcp.Implementation{
		Name:    "kanban-mcp",
		Version: "0.1.0",
	}, nil)
	s.register(srv)
	return srv, s, nil
}

func (s *Server) register(srv *mcp.Server) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: "board_view",
		Description: "Render the whole kanban board as columns x swim lanes: the single pane of glass. " +
			"Use to see current state before planning or moving work.",
	}, s.boardView)

	mcp.AddTool(srv, &mcp.Tool{
		Name: "item_create",
		Description: "Create a work item (epic|story|task|bug|spike). Epics contain stories; stories " +
			"contain tasks (set parent_id to nest). New items default to the 'backlog' column and the " +
			"calling agent's swim lane.",
	}, s.itemCreate)

	mcp.AddTool(srv, &mcp.Tool{
		Name: "item_move",
		Description: "Move an item to a workflow column (backlog|specifying|specd|in_progress|review|done) " +
			"and optionally a different swim lane. Respects WIP limits.",
	}, s.itemMove)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "item_set_spec",
		Description: "Set the spec reference (path/URL) and spec status (missing|draft|approved) for SDD tracking.",
	}, s.itemSetSpec)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "item_set_blocked",
		Description: "Flag or unflag an item as blocked, with a reason. Blocked is orthogonal to its column.",
	}, s.itemSetBlocked)

	mcp.AddTool(srv, &mcp.Tool{
		Name: "item_label",
		Description: "Add namespaced labels (type:, priority:, spec:, stage:, agent:, area:). Enum-validated " +
			"to keep the taxonomy consistent. Pass labels as 'ns:value' strings.",
	}, s.itemLabel)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "item_comment",
		Description: "Append a comment to an item's activity log (captures 'what's discussed' in this run).",
	}, s.itemComment)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "lane_configure",
		Description: "Create or ensure a swim lane. Lanes are configurable per agent; default is one per agent.",
	}, s.laneConfigure)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "items_list",
		Description: "List items with optional filters (column, lane, parent_id, kind, blocked). For drilling into a subtree.",
	}, s.itemsList)

	mcp.AddTool(srv, &mcp.Tool{
		Name: "board_export",
		Description: "Export the full board as a renderer-agnostic Snapshot (schema-versioned JSON: plan, " +
			"columns, lanes, items, precomputed cell grid, stats). Feed this to a UI generator to render a " +
			"custom visualization on demand, or to any external renderer.",
	}, s.boardExport)

	mcp.AddTool(srv, &mcp.Tool{
		Name: "docket_sync",
		Description: "Import a docket backlog into this board (idempotent). Reads change manifests from the " +
			"docket docs directory (e.g. <repo>/.docket/docs) and maps each change to a card keyed by docket id; " +
			"re-running updates in place. Works with any harness because docket stores work as markdown on a branch.",
	}, s.docketSync)
}

// ---- tool I/O types ----

type boardViewIn struct {
	LaneKey string `json:"lane_key,omitempty" jsonschema:"restrict the view to a single swim lane"`
}

type boardViewOut struct {
	Plan    string             `json:"plan"`
	Columns []string           `json:"columns"`
	Lanes   []string           `json:"lanes"`
	Cells   map[string][]cell  `json:"cells"` // key "lane|column" -> items
	Summary string             `json:"summary"`
}

type cell struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	Title    string `json:"title"`
	Priority string `json:"priority"`
	Spec     string `json:"spec_status"`
	Blocked  bool   `json:"blocked"`
	Parent   string `json:"parent_id,omitempty"`
}

func (s *Server) boardView(ctx context.Context, _ *mcp.CallToolRequest, in boardViewIn) (*mcp.CallToolResult, boardViewOut, error) {
	cols, err := s.st.Columns(ctx, s.plan.ID)
	if err != nil {
		return nil, boardViewOut{}, err
	}
	lanes, err := s.st.Lanes(ctx, s.plan.ID)
	if err != nil {
		return nil, boardViewOut{}, err
	}
	items, err := s.st.ListItems(ctx, s.plan.ID, store.Filter{LaneKey: in.LaneKey})
	if err != nil {
		return nil, boardViewOut{}, err
	}

	out := boardViewOut{Plan: s.plan.Name, Cells: map[string][]cell{}}
	for _, c := range cols {
		out.Columns = append(out.Columns, c.Key)
	}
	for _, l := range lanes {
		if in.LaneKey != "" && l.Key != in.LaneKey {
			continue
		}
		out.Lanes = append(out.Lanes, l.Key)
	}
	for _, it := range items {
		laneKey := it.LaneKey
		if laneKey == "" {
			laneKey = "shared"
		}
		key := laneKey + "|" + it.ColumnKey
		out.Cells[key] = append(out.Cells[key], cell{
			ID: it.ID, Kind: string(it.Kind), Title: it.Title,
			Priority: it.Priority, Spec: string(it.SpecStatus),
			Blocked: it.Blocked, Parent: it.ParentID,
		})
	}
	out.Summary = fmt.Sprintf("%d items across %d columns x %d lanes",
		len(items), len(out.Columns), len(out.Lanes))
	return nil, out, nil
}

type itemCreateIn struct {
	Kind     string   `json:"kind" jsonschema:"one of epic|story|task|bug|spike"`
	Title    string   `json:"title"`
	Body     string   `json:"body,omitempty"`
	ParentID string   `json:"parent_id,omitempty" jsonschema:"id of the containing epic/story to nest under"`
	Column   string   `json:"column,omitempty" jsonschema:"workflow column key; defaults to backlog"`
	Lane     string   `json:"lane,omitempty" jsonschema:"swim lane key; defaults to the calling agent's lane"`
	Priority string   `json:"priority,omitempty" jsonschema:"p0|p1|p2|p3; defaults to p2"`
	SpecRef  string   `json:"spec_ref,omitempty"`
	Labels   []string `json:"labels,omitempty" jsonschema:"namespaced labels as ns:value strings"`
}

type itemOut struct {
	ID      string `json:"id"`
	Message string `json:"message"`
}

func (s *Server) itemCreate(ctx context.Context, _ *mcp.CallToolRequest, in itemCreateIn) (*mcp.CallToolResult, itemOut, error) {
	labels, err := parseLabels(in.Labels)
	if err != nil {
		return nil, itemOut{}, err
	}
	lane := in.Lane
	if lane == "" {
		lane = s.defaultLane()
	}
	it := &board.Item{
		PlanID:    s.plan.ID,
		ParentID:  in.ParentID,
		Kind:      board.Kind(in.Kind),
		Title:     in.Title,
		Body:      in.Body,
		ColumnKey: in.Column,
		LaneKey:   lane,
		Priority:  in.Priority,
		SpecRef:   in.SpecRef,
		Labels:    labels,
	}
	created, err := s.st.CreateItem(ctx, s.agentID, it)
	if err != nil {
		return nil, itemOut{}, err
	}
	return nil, itemOut{ID: created.ID, Message: fmt.Sprintf("created %s %q", created.Kind, created.Title)}, nil
}

type itemMoveIn struct {
	ItemID string `json:"item_id"`
	Column string `json:"column" jsonschema:"target workflow column key"`
	Lane   string `json:"lane,omitempty" jsonschema:"optional target swim lane key"`
}

func (s *Server) itemMove(ctx context.Context, _ *mcp.CallToolRequest, in itemMoveIn) (*mcp.CallToolResult, itemOut, error) {
	if err := s.st.MoveItem(ctx, s.agentID, in.ItemID, in.Column, in.Lane); err != nil {
		return nil, itemOut{}, err
	}
	return nil, itemOut{ID: in.ItemID, Message: "moved to " + in.Column}, nil
}

type itemSetSpecIn struct {
	ItemID  string `json:"item_id"`
	SpecRef string `json:"spec_ref" jsonschema:"path or URL to the spec document"`
	Status  string `json:"status" jsonschema:"missing|draft|approved"`
}

func (s *Server) itemSetSpec(ctx context.Context, _ *mcp.CallToolRequest, in itemSetSpecIn) (*mcp.CallToolResult, itemOut, error) {
	if err := s.st.SetSpec(ctx, s.agentID, in.ItemID, in.SpecRef, board.SpecStatus(in.Status)); err != nil {
		return nil, itemOut{}, err
	}
	return nil, itemOut{ID: in.ItemID, Message: "spec set to " + in.Status}, nil
}

type itemSetBlockedIn struct {
	ItemID  string `json:"item_id"`
	Blocked bool   `json:"blocked"`
	Reason  string `json:"reason,omitempty"`
}

func (s *Server) itemSetBlocked(ctx context.Context, _ *mcp.CallToolRequest, in itemSetBlockedIn) (*mcp.CallToolResult, itemOut, error) {
	if err := s.st.SetBlocked(ctx, s.agentID, in.ItemID, in.Blocked, in.Reason); err != nil {
		return nil, itemOut{}, err
	}
	msg := "unblocked"
	if in.Blocked {
		msg = "blocked: " + in.Reason
	}
	return nil, itemOut{ID: in.ItemID, Message: msg}, nil
}

type itemLabelIn struct {
	ItemID string   `json:"item_id"`
	Labels []string `json:"labels" jsonschema:"namespaced labels as ns:value strings"`
}

func (s *Server) itemLabel(ctx context.Context, _ *mcp.CallToolRequest, in itemLabelIn) (*mcp.CallToolResult, itemOut, error) {
	labels, err := parseLabels(in.Labels)
	if err != nil {
		return nil, itemOut{}, err
	}
	if err := s.st.AddLabels(ctx, s.agentID, in.ItemID, labels); err != nil {
		return nil, itemOut{}, err
	}
	return nil, itemOut{ID: in.ItemID, Message: "labeled"}, nil
}

type itemCommentIn struct {
	ItemID string `json:"item_id"`
	Text   string `json:"text"`
}

func (s *Server) itemComment(ctx context.Context, _ *mcp.CallToolRequest, in itemCommentIn) (*mcp.CallToolResult, itemOut, error) {
	if err := s.st.AddComment(ctx, s.agentID, in.ItemID, in.Text); err != nil {
		return nil, itemOut{}, err
	}
	return nil, itemOut{ID: in.ItemID, Message: "comment added"}, nil
}

type laneConfigureIn struct {
	Key     string `json:"key" jsonschema:"stable lane key, e.g. an agent id"`
	Name    string `json:"name,omitempty"`
	AgentID string `json:"agent_id,omitempty"`
}

func (s *Server) laneConfigure(ctx context.Context, _ *mcp.CallToolRequest, in laneConfigureIn) (*mcp.CallToolResult, itemOut, error) {
	name := in.Name
	if name == "" {
		name = in.Key
	}
	if _, err := s.st.EnsureLane(ctx, s.plan.ID, board.Lane{Key: in.Key, Name: name, AgentID: in.AgentID}); err != nil {
		return nil, itemOut{}, err
	}
	return nil, itemOut{ID: in.Key, Message: "lane ready"}, nil
}

type itemsListIn struct {
	Column   string `json:"column,omitempty"`
	Lane     string `json:"lane,omitempty"`
	ParentID string `json:"parent_id,omitempty"`
	Kind     string `json:"kind,omitempty"`
}

type itemsListOut struct {
	Items []board.Item `json:"items"`
}

func (s *Server) itemsList(ctx context.Context, _ *mcp.CallToolRequest, in itemsListIn) (*mcp.CallToolResult, itemsListOut, error) {
	items, err := s.st.ListItems(ctx, s.plan.ID, store.Filter{
		ColumnKey: in.Column, LaneKey: in.Lane, ParentID: in.ParentID, Kind: in.Kind,
	})
	if err != nil {
		return nil, itemsListOut{}, err
	}
	return nil, itemsListOut{Items: items}, nil
}

type boardExportIn struct{}

func (s *Server) boardExport(ctx context.Context, _ *mcp.CallToolRequest, _ boardExportIn) (*mcp.CallToolResult, board.Snapshot, error) {
	snap, err := s.st.Snapshot(ctx, s.plan.ID)
	if err != nil {
		return nil, board.Snapshot{}, err
	}
	return nil, snap, nil
}

type docketSyncIn struct {
	DocsDir string `json:"docs_dir" jsonschema:"path to the docket docs directory, e.g. /path/to/repo/.docket/docs"`
}

type docketSyncOut struct {
	Changes int    `json:"changes"`
	Lanes   int    `json:"lanes"`
	Message string `json:"message"`
}

func (s *Server) docketSync(ctx context.Context, _ *mcp.CallToolRequest, in docketSyncIn) (*mcp.CallToolResult, docketSyncOut, error) {
	res, err := docket.NewSyncer(s.st, s.plan.ID).Sync(ctx, in.DocsDir)
	if err != nil {
		return nil, docketSyncOut{}, err
	}
	return nil, docketSyncOut{
		Changes: res.Changes, Lanes: res.Lanes,
		Message: fmt.Sprintf("synced %d docket changes into %d type lanes", res.Changes, res.Lanes),
	}, nil
}

// defaultLane picks the lane an item lands in when none is given, based on the
// profile's lane dimension: the agent's own lane under an agent profile, else the
// shared lane (epic/class-of-service profiles expect an explicit lane).
func (s *Server) defaultLane() string {
	if s.plan.LaneDimension == string(board.LaneByAgent) {
		return s.agentID
	}
	return "shared"
}

// parseLabels turns "ns:value" strings into board.Label.
func parseLabels(raw []string) ([]board.Label, error) {
	var out []board.Label
	for _, r := range raw {
		ns, val, ok := strings.Cut(r, ":")
		if !ok {
			return nil, fmt.Errorf("label %q must be in ns:value form", r)
		}
		out = append(out, board.Label{NS: strings.TrimSpace(ns), Value: strings.TrimSpace(val)})
	}
	return out, nil
}
