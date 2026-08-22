// Package mcpserver exposes the kanban board as MCP tools. Each tool maps to a
// store operation with a typed, JSON-schema'd input so agents get validation for
// free. The design goal is a single pane of glass for SDD / Spec-DD work.
package mcpserver

import (
	"context"
	"fmt"
	"strings"

	"github.com/ethanhinson/kanban-mcp/internal/board"
	"github.com/ethanhinson/kanban-mcp/internal/source"
	"github.com/ethanhinson/kanban-mcp/internal/store"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Server binds the store and the calling agent's identity to the MCP tools. It is
// NOT pinned to one board: a database hosts many boards (plans), and every item
// tool names its board_id explicitly. The agent creates/selects a board at
// runtime with board_start. defaultBoardID is the board seeded at construction
// (used to wire the viz layer and as a back-compat single-board default).
type Server struct {
	st             *store.Store
	agentID        string // this server instance's owning agent; event attribution + default lane
	defaultProfile string // profile used when board_start omits one
	defaultBoardID string // board seeded at New() time; what PlanID() reports
}

// PlanID returns the id of the board this server seeded at construction, or ""
// when nothing was seeded (no planName given). Used to wire the viz default board;
// other boards created via board_start are addressed by their own ids.
func (s *Server) PlanID() string { return s.defaultBoardID }

// New builds an MCP server over the given db. agentID identifies the agent driving
// this instance. profileKey is the default methodology (sdd|scrum|kanban|...) used
// for any board_start that omits a profile.
//
// planName controls the connect-time seed: pass a name to seed that one board
// (single-board / back-compat use, reported via PlanID()), or pass "" to seed
// NOTHING — the runtime-board model, where the agent creates boards on demand with
// board_start and no empty default board is left lying around.
func New(ctx context.Context, st *store.Store, planName, agentID, profileKey string) (*mcp.Server, *Server, error) {
	if agentID == "" {
		agentID = "agent"
	}
	if profileKey == "" {
		profileKey = "sdd"
	}

	s := &Server{st: st, agentID: agentID, defaultProfile: profileKey}
	if planName != "" {
		plan, err := st.CreatePlan(ctx, planName, "", profileKey)
		if err != nil {
			return nil, nil, err
		}
		s.defaultBoardID = plan.ID
		if err := s.ensureAgentLane(ctx, plan); err != nil {
			return nil, nil, err
		}
	}
	srv := mcp.NewServer(&mcp.Implementation{
		Name:    "kanban-mcp",
		Version: "0.1.0",
	}, nil)
	s.register(srv)
	return srv, s, nil
}

// ensureAgentLane auto-creates a per-agent lane, but only when the board's profile
// makes the agent the lane dimension — under epic/class-of-service profiles the
// agent isn't the lane.
func (s *Server) ensureAgentLane(ctx context.Context, plan *board.Plan) error {
	if plan.LaneDimension != string(board.LaneByAgent) {
		return nil
	}
	_, err := s.st.EnsureLane(ctx, plan.ID, board.Lane{Key: s.agentID, Name: s.agentID, AgentID: s.agentID})
	return err
}

// resolveBoard validates a board_id and returns the plan. A clear error tells the
// agent to call board_start when the id is empty or unknown — there is no hidden
// "active board", so every item tool must name one.
func (s *Server) resolveBoard(ctx context.Context, boardID string) (*board.Plan, error) {
	if boardID == "" {
		return nil, fmt.Errorf("board_id is required — call board_start first to create or select a board")
	}
	plan, err := s.st.LoadPlan(ctx, boardID)
	if err != nil {
		return nil, fmt.Errorf("unknown board_id %q — call board_start (or board_list to see boards)", boardID)
	}
	return plan, nil
}

func (s *Server) register(srv *mcp.Server) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: "board_start",
		Description: "Start a board: create a named board (or select it if it already exists) and get " +
			"back its board_id. This is the entry point — call it first, then pass the returned board_id to " +
			"every other tool. Optionally pick a methodology profile (sdd|scrum|kanban); defaults to the " +
			"server default. Idempotent by name, so re-starting the same name re-selects the same board.",
	}, s.boardStart)

	mcp.AddTool(srv, &mcp.Tool{
		Name: "board_list",
		Description: "List the boards in this database (id, name, profile, item count) so you can pick a " +
			"board_id to work against or resume an earlier one.",
	}, s.boardList)

	mcp.AddTool(srv, &mcp.Tool{
		Name: "board_delete",
		Description: "Delete a board (board_id) and everything on it — items, links, labels, comments. " +
			"Irreversible. Use to clean up a throwaway or finished session board.",
	}, s.boardDelete)

	mcp.AddTool(srv, &mcp.Tool{
		Name: "board_rename",
		Description: "Rename a board (board_id) to a new name. Names are unique across the db; renaming " +
			"to a name already in use is rejected.",
	}, s.boardRename)

	mcp.AddTool(srv, &mcp.Tool{
		Name: "board_view",
		Description: "Render one board (identified by board_id) as columns x swim lanes: the single pane of " +
			"glass. Use to see current state before planning or moving work.",
	}, s.boardView)

	mcp.AddTool(srv, &mcp.Tool{
		Name: "item_create",
		Description: "Create a work item on a board (board_id required): epic|story|task|bug|spike. Epics " +
			"contain stories; stories contain tasks (set parent_id to nest). New items default to the " +
			"'backlog' column and the calling agent's swim lane.",
	}, s.itemCreate)

	mcp.AddTool(srv, &mcp.Tool{
		Name: "item_link",
		Description: "Link two items on the same board with a first-class dependency: kind is " +
			"depends_on|related|discovered_from. Direction: from_id depends_on/relates_to/was_discovered_from " +
			"to_id. This is how the board expresses relationships — flat, not nested.",
	}, s.itemLink)

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
		Name: "source_sync",
		Description: "Sync an external source (currently 'docket') into a board (board_id required), " +
			"idempotently. Reads read-only backlog/done/ADR data from the source and projects it onto the " +
			"board as cards keyed by external id; re-running updates in place. The board process owns your " +
			"live in-flight work; sources are decoupled inputs. For docket, pass docs_dir " +
			"(e.g. <repo>/.docket/docs). Works with any harness because docket stores work as markdown on a branch.",
	}, s.sourceSync)
}

// ---- tool I/O types ----

type boardStartIn struct {
	Name    string `json:"name" jsonschema:"human name for the board, e.g. this session's deliverable"`
	Profile string `json:"profile,omitempty" jsonschema:"methodology profile sdd|scrum|kanban; defaults to the server default"`
}

type boardStartOut struct {
	BoardID string `json:"board_id"`
	Name    string `json:"name"`
	Profile string `json:"profile"`
	Message string `json:"message"`
}

func (s *Server) boardStart(ctx context.Context, _ *mcp.CallToolRequest, in boardStartIn) (*mcp.CallToolResult, boardStartOut, error) {
	if in.Name == "" {
		return nil, boardStartOut{}, fmt.Errorf("name is required")
	}
	profile := in.Profile
	if profile == "" {
		profile = s.defaultProfile
	}
	plan, err := s.st.CreatePlan(ctx, in.Name, "", profile)
	if err != nil {
		return nil, boardStartOut{}, err
	}
	if err := s.ensureAgentLane(ctx, plan); err != nil {
		return nil, boardStartOut{}, err
	}
	return nil, boardStartOut{
		BoardID: plan.ID, Name: plan.Name, Profile: plan.ProfileKey,
		Message: fmt.Sprintf("board %q ready — pass board_id=%s to the other tools", plan.Name, plan.ID),
	}, nil
}

type boardListIn struct{}

type boardListOut struct {
	Boards []store.PlanSummary `json:"boards"`
}

func (s *Server) boardList(ctx context.Context, _ *mcp.CallToolRequest, _ boardListIn) (*mcp.CallToolResult, boardListOut, error) {
	boards, err := s.st.ListPlans(ctx)
	if err != nil {
		return nil, boardListOut{}, err
	}
	return nil, boardListOut{Boards: boards}, nil
}

type boardDeleteIn struct {
	BoardID string `json:"board_id" jsonschema:"the board to delete (from board_list)"`
}

func (s *Server) boardDelete(ctx context.Context, _ *mcp.CallToolRequest, in boardDeleteIn) (*mcp.CallToolResult, itemOut, error) {
	plan, err := s.resolveBoard(ctx, in.BoardID)
	if err != nil {
		return nil, itemOut{}, err
	}
	if err := s.st.DeletePlan(ctx, plan.ID); err != nil {
		return nil, itemOut{}, err
	}
	return nil, itemOut{ID: plan.ID, Message: fmt.Sprintf("deleted board %q", plan.Name)}, nil
}

type boardRenameIn struct {
	BoardID string `json:"board_id" jsonschema:"the board to rename (from board_list)"`
	Name    string `json:"name" jsonschema:"the new board name (must be unique across the db)"`
}

func (s *Server) boardRename(ctx context.Context, _ *mcp.CallToolRequest, in boardRenameIn) (*mcp.CallToolResult, itemOut, error) {
	plan, err := s.resolveBoard(ctx, in.BoardID)
	if err != nil {
		return nil, itemOut{}, err
	}
	if err := s.st.RenamePlan(ctx, plan.ID, in.Name); err != nil {
		return nil, itemOut{}, err
	}
	return nil, itemOut{ID: plan.ID, Message: fmt.Sprintf("renamed %q -> %q", plan.Name, in.Name)}, nil
}

type boardViewIn struct {
	BoardID string `json:"board_id" jsonschema:"the board to view (from board_start / board_list)"`
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
	plan, err := s.resolveBoard(ctx, in.BoardID)
	if err != nil {
		return nil, boardViewOut{}, err
	}
	cols, err := s.st.Columns(ctx, plan.ID)
	if err != nil {
		return nil, boardViewOut{}, err
	}
	lanes, err := s.st.Lanes(ctx, plan.ID)
	if err != nil {
		return nil, boardViewOut{}, err
	}
	items, err := s.st.ListItems(ctx, plan.ID, store.Filter{LaneKey: in.LaneKey})
	if err != nil {
		return nil, boardViewOut{}, err
	}

	out := boardViewOut{Plan: plan.Name, Cells: map[string][]cell{}}
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
	BoardID  string   `json:"board_id" jsonschema:"the board to create the item on (from board_start)"`
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
	plan, err := s.resolveBoard(ctx, in.BoardID)
	if err != nil {
		return nil, itemOut{}, err
	}
	labels, err := parseLabels(in.Labels)
	if err != nil {
		return nil, itemOut{}, err
	}
	lane := in.Lane
	if lane == "" {
		lane = s.defaultLane(plan)
	}
	it := &board.Item{
		PlanID:    plan.ID,
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

type itemLinkIn struct {
	BoardID string `json:"board_id" jsonschema:"the board both items belong to"`
	FromID  string `json:"from_id" jsonschema:"the dependent/relating item"`
	ToID    string `json:"to_id" jsonschema:"the depended-on/related item"`
	Kind    string `json:"kind" jsonschema:"depends_on|related|discovered_from"`
}

func (s *Server) itemLink(ctx context.Context, _ *mcp.CallToolRequest, in itemLinkIn) (*mcp.CallToolResult, itemOut, error) {
	plan, err := s.resolveBoard(ctx, in.BoardID)
	if err != nil {
		return nil, itemOut{}, err
	}
	switch in.Kind {
	case "depends_on", "related", "discovered_from":
	default:
		return nil, itemOut{}, fmt.Errorf("kind %q must be depends_on|related|discovered_from", in.Kind)
	}
	for _, id := range []string{in.FromID, in.ToID} {
		pid, ok := s.st.ItemPlanID(ctx, id)
		if !ok {
			return nil, itemOut{}, fmt.Errorf("item %q not found", id)
		}
		if pid != plan.ID {
			return nil, itemOut{}, fmt.Errorf("item %q is not on board %q", id, plan.ID)
		}
	}
	if err := s.st.AddLink(ctx, plan.ID, in.FromID, in.ToID, in.Kind); err != nil {
		return nil, itemOut{}, err
	}
	return nil, itemOut{ID: in.FromID, Message: fmt.Sprintf("%s -> %s (%s)", in.FromID, in.ToID, in.Kind)}, nil
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
	BoardID string `json:"board_id" jsonschema:"the board to add/ensure the lane on"`
	Key     string `json:"key" jsonschema:"stable lane key, e.g. an agent id"`
	Name    string `json:"name,omitempty"`
	AgentID string `json:"agent_id,omitempty"`
}

func (s *Server) laneConfigure(ctx context.Context, _ *mcp.CallToolRequest, in laneConfigureIn) (*mcp.CallToolResult, itemOut, error) {
	plan, err := s.resolveBoard(ctx, in.BoardID)
	if err != nil {
		return nil, itemOut{}, err
	}
	name := in.Name
	if name == "" {
		name = in.Key
	}
	if _, err := s.st.EnsureLane(ctx, plan.ID, board.Lane{Key: in.Key, Name: name, AgentID: in.AgentID}); err != nil {
		return nil, itemOut{}, err
	}
	return nil, itemOut{ID: in.Key, Message: "lane ready"}, nil
}

type itemsListIn struct {
	BoardID  string `json:"board_id" jsonschema:"the board to list items from"`
	Column   string `json:"column,omitempty"`
	Lane     string `json:"lane,omitempty"`
	ParentID string `json:"parent_id,omitempty"`
	Kind     string `json:"kind,omitempty"`
}

type itemsListOut struct {
	Items []board.Item `json:"items"`
}

func (s *Server) itemsList(ctx context.Context, _ *mcp.CallToolRequest, in itemsListIn) (*mcp.CallToolResult, itemsListOut, error) {
	plan, err := s.resolveBoard(ctx, in.BoardID)
	if err != nil {
		return nil, itemsListOut{}, err
	}
	items, err := s.st.ListItems(ctx, plan.ID, store.Filter{
		ColumnKey: in.Column, LaneKey: in.Lane, ParentID: in.ParentID, Kind: in.Kind,
	})
	if err != nil {
		return nil, itemsListOut{}, err
	}
	return nil, itemsListOut{Items: items}, nil
}

type boardExportIn struct {
	BoardID string `json:"board_id" jsonschema:"the board to export"`
}

func (s *Server) boardExport(ctx context.Context, _ *mcp.CallToolRequest, in boardExportIn) (*mcp.CallToolResult, board.Snapshot, error) {
	plan, err := s.resolveBoard(ctx, in.BoardID)
	if err != nil {
		return nil, board.Snapshot{}, err
	}
	snap, err := s.st.Snapshot(ctx, plan.ID)
	if err != nil {
		return nil, board.Snapshot{}, err
	}
	return nil, snap, nil
}

type sourceSyncIn struct {
	BoardID string `json:"board_id" jsonschema:"the board to project the source onto"`
	Source  string `json:"source,omitempty" jsonschema:"external source kind; defaults to docket"`
	DocsDir string `json:"docs_dir" jsonschema:"for docket: path to the docs dir, e.g. /path/to/repo/.docket/docs"`
}

type sourceSyncOut struct {
	Source  string `json:"source"`
	Items   int    `json:"items"`
	Links   int    `json:"links"`
	Message string `json:"message"`
}

func (s *Server) sourceSync(ctx context.Context, _ *mcp.CallToolRequest, in sourceSyncIn) (*mcp.CallToolResult, sourceSyncOut, error) {
	plan, err := s.resolveBoard(ctx, in.BoardID)
	if err != nil {
		return nil, sourceSyncOut{}, err
	}
	kind := in.Source
	if kind == "" {
		kind = "docket"
	}
	provider, err := source.NewProvider(kind, source.Config{DocsDir: in.DocsDir})
	if err != nil {
		return nil, sourceSyncOut{}, err
	}
	res, err := source.Sync(ctx, s.st, plan.ID, provider)
	if err != nil {
		return nil, sourceSyncOut{}, err
	}
	return nil, sourceSyncOut{
		Source: kind, Items: res.Items, Links: res.Links,
		Message: fmt.Sprintf("synced %d %s items (%d links) onto board %q", res.Items, kind, res.Links, plan.Name),
	}, nil
}

// defaultLane picks the lane an item lands in when none is given, based on the
// board profile's lane dimension: the agent's own lane under an agent profile,
// else the shared lane (epic/class-of-service profiles expect an explicit lane).
func (s *Server) defaultLane(plan *board.Plan) string {
	if plan.LaneDimension == string(board.LaneByAgent) {
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
