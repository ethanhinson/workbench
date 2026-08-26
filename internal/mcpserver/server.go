// Package mcpserver exposes the kanban board as MCP tools. Each tool maps to a
// store operation with a typed, JSON-schema'd input so agents get validation for
// free. The design goal is a single pane of glass for SDD / Spec-DD work.
package mcpserver

import (
	"context"
	"fmt"
	"strings"

	"github.com/ethanhinson/workbench/internal/adapter"
	"github.com/ethanhinson/workbench/internal/board"
	"github.com/ethanhinson/workbench/internal/store"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Server binds the store and the calling agent's identity to the MCP tools. It is
// never pinned to a board: a database hosts many boards (plans), the agent creates
// or selects one at runtime with board_start, and every item tool names its
// board_id explicitly. No board is created at construction.
type Server struct {
	st             *store.Store
	agentID        string // this server instance's owning agent; event attribution + default lane
	defaultProfile string // profile used when board_start omits one
	defaultProject string // project boards land in when board_start omits one (a dir path)
}

// New builds an MCP server over the given db. agentID identifies the agent driving
// this instance. profileKey is the default methodology (sdd|scrum|kanban|docket|openspec|superpowers|...) used
// for any board_start that omits a profile. defaultProject is the project boards
// land in when board_start doesn't name one — by default the server's working
// directory, so one shared db groups boards by project.
func New(st *store.Store, agentID, profileKey, defaultProject string) (*mcp.Server, *Server) {
	if agentID == "" {
		agentID = "agent"
	}
	if profileKey == "" {
		profileKey = "sdd"
	}
	s := &Server{st: st, agentID: agentID, defaultProfile: profileKey, defaultProject: defaultProject}
	srv := mcp.NewServer(&mcp.Implementation{
		Name:    "workbench",
		Version: "0.1.0",
	}, nil)
	s.register(srv)
	return srv, s
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
			"every other tool. A board belongs to a project (a directory path); pass project (e.g. your " +
			"$CLAUDE_PROJECT_DIR) to group this session's boards under it, or omit it to use the server's " +
			"working directory. Optionally pick a methodology profile (sdd|scrum|kanban|docket|openspec|superpowers). Idempotent by " +
			"(project, name), so re-starting the same name in the same project re-selects the same board.",
	}, s.boardStart)

	mcp.AddTool(srv, &mcp.Tool{
		Name: "board_list",
		Description: "List boards (id, name, project, profile, item count) so you can pick a board_id to work " +
			"against or resume one. Pass project (a directory path) to list only that project's boards; omit " +
			"it to list every board grouped by project.",
	}, s.boardList)

	mcp.AddTool(srv, &mcp.Tool{
		Name: "board_delete",
		Description: "Delete a board (board_id) and everything on it — items, links, labels, comments. " +
			"Irreversible. Use to clean up a throwaway or finished session board.",
	}, s.boardDelete)

	mcp.AddTool(srv, &mcp.Tool{
		Name: "board_rename",
		Description: "Rename a board (board_id) to a new name. Names are unique within a project; renaming " +
			"to a name already in use in that project is rejected.",
	}, s.boardRename)

	mcp.AddTool(srv, &mcp.Tool{
		Name: "board_set_project",
		Description: "Move a board (board_id) to a different project (a directory path). Use to (re)assign a " +
			"board's project, e.g. to group an older board under its repo. Rejected if the target project " +
			"already has a board with this name.",
	}, s.boardSetProject)

	mcp.AddTool(srv, &mcp.Tool{
		Name: "board_set_layout",
		Description: "Define how a board renders: its nav tabs and views. A board has NO layout until this " +
			"is set (it renders empty). Pass a layout object: nav[] (tabs, each opening a view) + views{} " +
			"(each with type list|lanes|board|doc; lanes/columns for lanes|board). Items appear where their " +
			"view:/lane:/column: labels place them. Idempotent — replaces the layout. This is how a " +
			"methodology skill shapes a tool-idiomatic board.",
	}, s.boardSetLayout)

	mcp.AddTool(srv, &mcp.Tool{
		Name: "board_get_layout",
		Description: "Return a board's current layout (nav + views), or empty if none is set. Use to read " +
			"and tweak an existing layout rather than reauthoring it.",
	}, s.boardGetLayout)

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
		Name: "item_upsert",
		Description: "Create-or-update a card keyed by (board_id, ext_key) — the hydration primitive for " +
			"methodology skills. Re-running with the same ext_key UPDATES in place (never duplicates). Carry " +
			"content (the full doc markdown a doc view renders) and placement labels (view:<v>, lane:<l>, " +
			"column:<c>). Use as you work: whenever you touch a source artifact, upsert its card so the board " +
			"stays live. ext_key is a stable source id like 'openspec:auth' or 'docket:74'.",
	}, s.itemUpsert)

	mcp.AddTool(srv, &mcp.Tool{
		Name: "item_link",
		Description: "Link two items on the same board with a first-class dependency: kind is " +
			"depends_on|related|discovered_from. Direction: from_id depends_on/relates_to/was_discovered_from " +
			"to_id. This is how the board expresses relationships — flat, not nested.",
	}, s.itemLink)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "item_set_content",
		Description: "Replace an item's content (the full doc markdown a doc view renders). Use to refresh a card's document as you edit the underlying file.",
	}, s.itemSetContent)

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
		Description: "Re-project a repo's docket backlog (.docket/docs/changes) onto a board on demand. Reads " +
			"every change manifest, deterministically maps each to a card (keyed docket:<id>) by its " +
			"frontmatter, and reconciles deletes. Idempotent — safe to call repeatedly. Use when the " +
			"filesystem watcher isn't running (e.g. a viz-less MCP session) and you want the board refreshed.",
	}, s.docketSync)

}

// ---- tool I/O types ----

type boardStartIn struct {
	Name    string `json:"name" jsonschema:"human name for the board, e.g. this session's deliverable"`
	Profile string `json:"profile,omitempty" jsonschema:"methodology profile sdd|scrum|kanban|docket|openspec|superpowers; defaults to the server default"`
	Project string `json:"project,omitempty" jsonschema:"project this board belongs to (a directory path); defaults to the working directory. Pass your project root, e.g. $CLAUDE_PROJECT_DIR, to group this session's boards under it"`
}

type boardStartOut struct {
	BoardID string `json:"board_id"`
	Name    string `json:"name"`
	Project string `json:"project"`
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
	project := in.Project
	if project == "" {
		project = s.defaultProject
	}
	plan, err := s.st.CreatePlan(ctx, in.Name, project, "", profile)
	if err != nil {
		return nil, boardStartOut{}, err
	}
	if err := s.ensureAgentLane(ctx, plan); err != nil {
		return nil, boardStartOut{}, err
	}
	return nil, boardStartOut{
		BoardID: plan.ID, Name: plan.Name, Project: plan.Project, Profile: plan.ProfileKey,
		Message: fmt.Sprintf("board %q ready in project %q — pass board_id=%s to the other tools", plan.Name, plan.Project, plan.ID),
	}, nil
}

type boardListIn struct {
	Project string `json:"project,omitempty" jsonschema:"only list boards in this project (a directory path); omit to list every board grouped by project"`
}

type boardListOut struct {
	Boards []store.PlanSummary `json:"boards"`
}

func (s *Server) boardList(ctx context.Context, _ *mcp.CallToolRequest, in boardListIn) (*mcp.CallToolResult, boardListOut, error) {
	var boards []store.PlanSummary
	var err error
	if in.Project != "" {
		boards, err = s.st.ListPlansForProject(ctx, in.Project)
	} else {
		boards, err = s.st.ListPlans(ctx)
	}
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
	Name    string `json:"name" jsonschema:"the new board name (must be unique within the board's project)"`
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

type boardSetProjectIn struct {
	BoardID string `json:"board_id" jsonschema:"the board to move (from board_list)"`
	Project string `json:"project" jsonschema:"the target project (a directory path)"`
}

func (s *Server) boardSetProject(ctx context.Context, _ *mcp.CallToolRequest, in boardSetProjectIn) (*mcp.CallToolResult, itemOut, error) {
	plan, err := s.resolveBoard(ctx, in.BoardID)
	if err != nil {
		return nil, itemOut{}, err
	}
	if err := s.st.SetPlanProject(ctx, plan.ID, in.Project); err != nil {
		return nil, itemOut{}, err
	}
	return nil, itemOut{ID: plan.ID, Message: fmt.Sprintf("moved %q to project %q", plan.Name, in.Project)}, nil
}

type boardSetLayoutIn struct {
	BoardID string       `json:"board_id" jsonschema:"the board to lay out"`
	Layout  board.Layout `json:"layout" jsonschema:"the layout: nav[] tabs + views{} (each type list|lanes|board|doc)"`
}

func (s *Server) boardSetLayout(ctx context.Context, _ *mcp.CallToolRequest, in boardSetLayoutIn) (*mcp.CallToolResult, itemOut, error) {
	plan, err := s.resolveBoard(ctx, in.BoardID)
	if err != nil {
		return nil, itemOut{}, err
	}
	if err := s.st.SetPlanLayout(ctx, plan.ID, in.Layout); err != nil {
		return nil, itemOut{}, err
	}
	return nil, itemOut{ID: plan.ID, Message: fmt.Sprintf("layout set: %d nav tabs, %d views", len(in.Layout.Nav), len(in.Layout.Views))}, nil
}

type boardGetLayoutIn struct {
	BoardID string `json:"board_id" jsonschema:"the board whose layout to read"`
}

type boardGetLayoutOut struct {
	HasLayout bool         `json:"has_layout"`
	Layout    board.Layout `json:"layout"`
}

func (s *Server) boardGetLayout(ctx context.Context, _ *mcp.CallToolRequest, in boardGetLayoutIn) (*mcp.CallToolResult, boardGetLayoutOut, error) {
	plan, err := s.resolveBoard(ctx, in.BoardID)
	if err != nil {
		return nil, boardGetLayoutOut{}, err
	}
	lo, ok, err := s.st.GetPlanLayout(ctx, plan.ID)
	if err != nil {
		return nil, boardGetLayoutOut{}, err
	}
	// Keep maps/slices non-nil so the JSON output is object/array, not null.
	if lo.Views == nil {
		lo.Views = map[string]board.LayoutView{}
	}
	if lo.Nav == nil {
		lo.Nav = []board.NavItem{}
	}
	return nil, boardGetLayoutOut{HasLayout: ok, Layout: lo}, nil
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
	Content  string   `json:"content,omitempty" jsonschema:"full doc markdown a doc view renders (e.g. a spec/ADR body)"`
	ParentID string   `json:"parent_id,omitempty" jsonschema:"id of the containing epic/story to nest under"`
	Column   string   `json:"column,omitempty" jsonschema:"workflow column key; defaults to backlog"`
	Lane     string   `json:"lane,omitempty" jsonschema:"swim lane key; defaults to the calling agent's lane"`
	Priority string   `json:"priority,omitempty" jsonschema:"p0|p1|p2|p3; defaults to p2"`
	SpecRef  string   `json:"spec_ref,omitempty"`
	Labels   []string `json:"labels,omitempty" jsonschema:"namespaced labels as ns:value strings (incl. view:/lane:/column:)"`
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
		Content:   in.Content,
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

type itemUpsertIn struct {
	BoardID  string   `json:"board_id" jsonschema:"the board to upsert onto"`
	ExtKey   string   `json:"ext_key" jsonschema:"stable source id, e.g. 'openspec:auth' or 'docket:74' — the upsert key"`
	Title    string   `json:"title"`
	Kind     string   `json:"kind,omitempty" jsonschema:"epic|story|task|bug|spike; defaults to task"`
	Body     string   `json:"body,omitempty"`
	Content  string   `json:"content,omitempty" jsonschema:"full doc markdown a doc view renders"`
	Column   string   `json:"column,omitempty" jsonschema:"workflow column key; defaults to backlog"`
	Lane     string   `json:"lane,omitempty"`
	Priority string   `json:"priority,omitempty"`
	SpecRef  string   `json:"spec_ref,omitempty"`
	Labels   []string `json:"labels,omitempty" jsonschema:"namespaced labels incl. view:/lane:/column: for placement"`
}

func (s *Server) itemUpsert(ctx context.Context, _ *mcp.CallToolRequest, in itemUpsertIn) (*mcp.CallToolResult, itemOut, error) {
	plan, err := s.resolveBoard(ctx, in.BoardID)
	if err != nil {
		return nil, itemOut{}, err
	}
	if in.ExtKey == "" {
		return nil, itemOut{}, fmt.Errorf("ext_key is required for upsert")
	}
	labels, err := parseLabels(in.Labels)
	if err != nil {
		return nil, itemOut{}, err
	}
	kind := board.Kind(in.Kind)
	if kind == "" {
		kind = board.KindTask
	}
	lane := in.Lane
	if lane == "" {
		lane = s.defaultLane(plan)
	}
	it := &board.Item{
		PlanID:   plan.ID,
		Kind:     kind,
		Title:    in.Title,
		Body:     in.Body,
		Content:  in.Content,
		ColumnKey: in.Column,
		LaneKey:  lane,
		Priority: in.Priority,
		SpecRef:  in.SpecRef,
		ExtKey:   in.ExtKey,
		Labels:   labels,
	}
	saved, err := s.st.UpsertByExtKey(ctx, s.agentID, it)
	if err != nil {
		return nil, itemOut{}, err
	}
	return nil, itemOut{ID: saved.ID, Message: fmt.Sprintf("upserted %s", in.ExtKey)}, nil
}

type itemSetContentIn struct {
	ItemID  string `json:"item_id"`
	Content string `json:"content" jsonschema:"the full doc markdown to store on the item"`
}

func (s *Server) itemSetContent(ctx context.Context, _ *mcp.CallToolRequest, in itemSetContentIn) (*mcp.CallToolResult, itemOut, error) {
	if err := s.st.SetContent(ctx, s.agentID, in.ItemID, in.Content); err != nil {
		return nil, itemOut{}, err
	}
	return nil, itemOut{ID: in.ItemID, Message: fmt.Sprintf("content set (%d bytes)", len(in.Content))}, nil
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

// defaultLane picks the lane an item lands in when none is given, based on the
// board profile's lane dimension: the agent's own lane under an agent profile,
// else the shared lane (epic/class-of-service profiles expect an explicit lane).
func (s *Server) defaultLane(plan *board.Plan) string {
	if plan.LaneDimension == string(board.LaneByAgent) {
		return s.agentID
	}
	return "shared"
}

type docketSyncIn struct {
	BoardID string `json:"board_id" jsonschema:"the board to sync onto (from board_start)"`
	RepoDir string `json:"repo_dir,omitempty" jsonschema:"repo root containing .docket/docs/changes; defaults to the server's project"`
}

// docketSync re-projects a repo's docket backlog onto a board on demand.
func (s *Server) docketSync(ctx context.Context, _ *mcp.CallToolRequest, in docketSyncIn) (*mcp.CallToolResult, itemOut, error) {
	plan, err := s.resolveBoard(ctx, in.BoardID)
	if err != nil {
		return nil, itemOut{}, err
	}
	repo := in.RepoDir
	if repo == "" {
		repo = s.defaultProject
	}
	a, ok := adapter.Detect(repo)
	if !ok {
		return nil, itemOut{}, fmt.Errorf("no docket footprint under %q", repo)
	}
	if err := a.Sync(ctx, s.st, plan.ID, repo); err != nil {
		return nil, itemOut{}, err
	}
	return nil, itemOut{ID: plan.ID, Message: fmt.Sprintf("docket synced onto %q", plan.Name)}, nil
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
