// Package store is the SQLite persistence layer for Workbench.
// One database file == one Plan (the shared top-level board). Concurrent agents
// are supported via WAL + busy_timeout configured in schema.sql.
package store

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ethanhinson/workbench/internal/board"
	"github.com/oklog/ulid/v2"
	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schemaSQL string

// Store wraps the SQLite connection.
type Store struct {
	db     *sql.DB
	broker *Broker
}

// Open opens (creating if needed) the plan database at path and applies the schema.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	// modernc sqlite is safe for concurrent use; a small pool avoids "database locked".
	db.SetMaxOpenConns(1) // single writer keeps WAL semantics simple for multi-agent writes
	if _, err := db.ExecContext(context.Background(), schemaSQL); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	return &Store{db: db, broker: newBroker()}, nil
}

// Broker returns the store's change-notification broker. Consumers (the SSE
// handler, a local TUI) Subscribe to be woken on every mutation.
func (s *Store) Broker() *Broker { return s.broker }

// commit commits the transaction and, on success, notifies subscribers that the
// board changed. All mutating methods route their commit through here so the
// live-update signal can never be forgotten.
func (s *Store) commit(tx *sql.Tx) error {
	if err := tx.Commit(); err != nil {
		return err
	}
	s.broker.notify()
	return nil
}

func (s *Store) Close() error { return s.db.Close() }

func now() string    { return time.Now().UTC().Format(time.RFC3339) }
func newID() string  { return ulid.Make().String() }

// CreatePlan creates a board (plan) in a project from the named methodology
// profile, seeding its columns, seed lanes, lane dimension, and enforcement
// policies — the profile is what binds meaning to both board axes (see
// board.Profile). It is idempotent by (project, name): starting a board that
// already exists in that project returns the existing one rather than erroring, so
// the same name can exist under different projects. An unknown profileKey falls
// back to "sdd". This is the store primitive behind the agent-facing board_start.
func (s *Store) CreatePlan(ctx context.Context, name, project, description, profileKey string) (*board.Plan, error) {
	if existing, err := s.GetPlanByName(ctx, project, name); err == nil {
		return existing, nil // idempotent: board_start on an existing (project,name) selects it
	} else if err != sql.ErrNoRows {
		return nil, err
	}

	prof, ok := board.LookupProfile(profileKey)
	if !ok {
		prof, _ = board.LookupProfile("sdd")
	}
	policiesJSON, err := json.Marshal(prof.Policies)
	if err != nil {
		return nil, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	ts := now()
	p := board.Plan{
		ID: newID(), Name: name, Project: project, Description: description,
		ProfileKey: prof.Key, LaneDimension: string(prof.LaneDimension), PoliciesJSON: string(policiesJSON),
		CreatedAt: ts, UpdatedAt: ts,
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO plan(id,name,project,description,profile,lane_dim,policies,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?)`,
		p.ID, p.Name, p.Project, p.Description, p.ProfileKey, p.LaneDimension, p.PoliciesJSON, p.CreatedAt, p.UpdatedAt); err != nil {
		return nil, err
	}
	for _, c := range prof.Columns {
		var wip any
		if c.WIPLimit != nil {
			wip = *c.WIPLimit
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO column_def(id,plan_id,key,name,position,is_done,wip_limit) VALUES(?,?,?,?,?,?,?)`,
			newID(), p.ID, c.Key, c.Name, c.Position, boolInt(c.IsDone), wip); err != nil {
			return nil, err
		}
	}
	// Always a shared lane so items are never orphaned, plus any profile seed lanes.
	seedLanes := append([]board.Lane{{Key: "shared", Name: "Shared", Position: 0}}, prof.SeedLanes...)
	for i, l := range seedLanes {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO lane(id,plan_id,key,name,agent_id,position) VALUES(?,?,?,?,?,?)`,
			newID(), p.ID, l.Key, l.Name, l.AgentID, i); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &p, nil
}

// GetPlanByName resolves a board by (project, name) — names are unique within a
// project. Returns sql.ErrNoRows if absent (callers distinguish "absent" from a
// real error).
func (s *Store) GetPlanByName(ctx context.Context, project, name string) (*board.Plan, error) {
	var p board.Plan
	err := s.db.QueryRowContext(ctx,
		`SELECT id,name,project,description,profile,lane_dim,policies,created_at,updated_at FROM plan WHERE project=? AND name=?`, project, name).
		Scan(&p.ID, &p.Name, &p.Project, &p.Description, &p.ProfileKey, &p.LaneDimension, &p.PoliciesJSON, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// PlanSummary is a lightweight board listing entry (for board_list / a UI picker).
type PlanSummary struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Project    string `json:"project"`
	ProfileKey string `json:"profile"`
	Items      int    `json:"items"`
	CreatedAt  string `json:"created_at"`
}

// ListPlans returns boards with a cheap item count, newest last. When project is
// non-empty it returns only that project's boards; "" returns every board (grouped
// by project for a UI to render). ByProject controls the filter explicitly so ""
// can still mean the default/unnamed project vs "all".
func (s *Store) ListPlans(ctx context.Context) ([]PlanSummary, error) {
	return s.listPlans(ctx, "", false)
}

// ListPlansForProject returns only the given project's boards.
func (s *Store) ListPlansForProject(ctx context.Context, project string) ([]PlanSummary, error) {
	return s.listPlans(ctx, project, true)
}

func (s *Store) listPlans(ctx context.Context, project string, filter bool) ([]PlanSummary, error) {
	// The count is work tickets only — activity-feed events (a view:activity label)
	// are a session's tool-call log, not board work, and would inflate the number
	// with a different kind of thing (a docket board of 6 changes reading "43").
	q := `SELECT p.id, p.name, p.project, p.profile, p.created_at,
	             COUNT(i.id) FILTER (WHERE i.id NOT IN (
	               SELECT item_id FROM label WHERE ns='view' AND value='activity'))
	      FROM plan p LEFT JOIN item i ON i.plan_id = p.id`
	var args []any
	if filter {
		q += ` WHERE p.project = ?`
		args = append(args, project)
	}
	q += ` GROUP BY p.id ORDER BY p.project, p.created_at`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PlanSummary
	for rows.Next() {
		var ps PlanSummary
		if err := rows.Scan(&ps.ID, &ps.Name, &ps.Project, &ps.ProfileKey, &ps.CreatedAt, &ps.Items); err != nil {
			return nil, err
		}
		out = append(out, ps)
	}
	return out, rows.Err()
}

// DeletePlan removes a board and everything under it (items, links, labels,
// events, lanes, columns) in one transaction. Children are deleted explicitly
// rather than relying on ON DELETE CASCADE, so it's correct even if foreign-key
// enforcement is off on the connection. Returns an error if the board is absent.
func (s *Store) DeletePlan(ctx context.Context, planID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(1) FROM plan WHERE id=?`, planID).Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		return fmt.Errorf("board %q not found", planID)
	}

	// Labels hang off items, so clear them first (by the plan's items), then the
	// rest. Order matters only for label (no plan_id column of its own).
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM label WHERE item_id IN (SELECT id FROM item WHERE plan_id=?)`, planID); err != nil {
		return err
	}
	for _, table := range []string{"event", "link", "item", "lane", "column_def"} {
		if _, err := tx.ExecContext(ctx, `DELETE FROM `+table+` WHERE plan_id=?`, planID); err != nil {
			return fmt.Errorf("delete from %s: %w", table, err)
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM plan WHERE id=?`, planID); err != nil {
		return fmt.Errorf("delete plan: %w", err)
	}
	return s.commit(tx)
}

// RenamePlan changes a board's name. Names are unique within a project, so a clash
// is checked against the board's OWN project and returns a clear error rather than
// a raw constraint failure. Returns an error if the board doesn't exist.
func (s *Store) RenamePlan(ctx context.Context, planID, newName string) error {
	if newName == "" {
		return fmt.Errorf("new board name is required")
	}
	var project string
	if err := s.db.QueryRowContext(ctx, `SELECT project FROM plan WHERE id=?`, planID).Scan(&project); err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("board %q not found", planID)
		}
		return err
	}
	// Reject a name already taken by a different board in the same project.
	var clashID string
	err := s.db.QueryRowContext(ctx, `SELECT id FROM plan WHERE project=? AND name=?`, project, newName).Scan(&clashID)
	switch {
	case err == nil && clashID != planID:
		return fmt.Errorf("a board named %q already exists in this project", newName)
	case err == nil:
		return nil // same board already has this name: no-op
	case err != sql.ErrNoRows:
		return err
	}
	if _, err := s.db.ExecContext(ctx,
		`UPDATE plan SET name=?, updated_at=? WHERE id=?`, newName, now(), planID); err != nil {
		return err
	}
	s.broker.notify() // live UI + sidebar reflect the new name
	return nil
}

// SetPlanProject moves a board to a different project. Because board names are
// unique within a project, the move is rejected if the target project already has
// a board with this board's name (a raw constraint failure would otherwise be
// opaque). Moving a board to the project it's already in is a no-op. Returns an
// error if the board doesn't exist.
func (s *Store) SetPlanProject(ctx context.Context, planID, newProject string) error {
	var name, curProject string
	err := s.db.QueryRowContext(ctx, `SELECT name, project FROM plan WHERE id=?`, planID).Scan(&name, &curProject)
	if err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("board %q not found", planID)
		}
		return err
	}
	if newProject == curProject {
		return nil // already there
	}
	// Reject if another board in the target project already has this name.
	var clashID string
	err = s.db.QueryRowContext(ctx, `SELECT id FROM plan WHERE project=? AND name=?`, newProject, name).Scan(&clashID)
	switch {
	case err == nil && clashID != planID:
		return fmt.Errorf("project %q already has a board named %q", newProject, name)
	case err != nil && err != sql.ErrNoRows:
		return err
	}
	if _, err := s.db.ExecContext(ctx,
		`UPDATE plan SET project=?, updated_at=? WHERE id=?`, newProject, now(), planID); err != nil {
		return err
	}
	s.broker.notify() // live UI + sidebar re-group under the new project
	return nil
}

// SetPlanLayout stores a board's agent-authored layout (validated JSON). It fully
// replaces any prior layout. Returns an error if the board doesn't exist.
func (s *Store) SetPlanLayout(ctx context.Context, planID string, lo board.Layout) error {
	if err := lo.Validate(); err != nil {
		return err
	}
	raw, err := json.Marshal(lo)
	if err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE plan SET layout=?, updated_at=? WHERE id=?`, string(raw), now(), planID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("board %q not found", planID)
	}
	s.broker.notify() // live UI re-renders under the new layout
	return nil
}

// GetPlanLayout returns a board's layout and whether one is set. ok is false (and
// the Layout zero-valued) when the board has no layout yet.
func (s *Store) GetPlanLayout(ctx context.Context, planID string) (board.Layout, bool, error) {
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT layout FROM plan WHERE id=?`, planID).Scan(&raw)
	if err != nil {
		if err == sql.ErrNoRows {
			return board.Layout{}, false, fmt.Errorf("board %q not found", planID)
		}
		return board.Layout{}, false, err
	}
	if raw == "" {
		return board.Layout{}, false, nil
	}
	var lo board.Layout
	if err := json.Unmarshal([]byte(raw), &lo); err != nil {
		return board.Layout{}, false, fmt.Errorf("decode layout: %w", err)
	}
	return lo, true, nil
}

// LoadPlan returns the single plan row with all fields populated.
func (s *Store) LoadPlan(ctx context.Context, planID string) (*board.Plan, error) {
	var p board.Plan
	err := s.db.QueryRowContext(ctx,
		`SELECT id,name,project,description,profile,lane_dim,policies,created_at,updated_at FROM plan WHERE id=?`, planID).
		Scan(&p.ID, &p.Name, &p.Project, &p.Description, &p.ProfileKey, &p.LaneDimension, &p.PoliciesJSON, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// Snapshot assembles the renderer-agnostic board.Snapshot for a plan — the
// pluggable contract every UI (SPA, generated component, TUI, export) consumes.
func (s *Store) Snapshot(ctx context.Context, planID string) (board.Snapshot, error) {
	plan, err := s.LoadPlan(ctx, planID)
	if err != nil {
		return board.Snapshot{}, err
	}
	cols, err := s.Columns(ctx, planID)
	if err != nil {
		return board.Snapshot{}, err
	}
	lanes, err := s.Lanes(ctx, planID)
	if err != nil {
		return board.Snapshot{}, err
	}
	items, err := s.ListItems(ctx, planID, Filter{})
	if err != nil {
		return board.Snapshot{}, err
	}
	links, err := s.Links(ctx, planID)
	if err != nil {
		return board.Snapshot{}, err
	}
	layout, hasLayout, err := s.GetPlanLayout(ctx, planID)
	if err != nil {
		return board.Snapshot{}, err
	}
	return board.BuildSnapshot(*plan, layout, hasLayout, cols, lanes, items, links), nil
}

// ItemDetail assembles the full click-through detail for one item: the item (with
// its content) and its bidirectional dependency refs. Content is whatever the agent
// stored on the item — the server never reads the filesystem.
func (s *Store) ItemDetail(ctx context.Context, planID, itemID string) (board.ItemDetail, error) {
	it, err := s.getItem(ctx, itemID)
	if err != nil {
		return board.ItemDetail{}, err
	}
	labels, err := s.itemLabels(ctx, itemID)
	if err != nil {
		return board.ItemDetail{}, err
	}
	it.Labels = labels
	d := board.ItemDetail{Item: *it}

	// refFor resolves an item id to a lightweight ref. Called AFTER all cursors are
	// closed — the pool is MaxOpenConns(1), so querying while a rows cursor is open
	// would deadlock.
	refFor := func(id string) board.LinkedRef {
		ref := board.LinkedRef{ID: id}
		var extKey sql.NullString
		s.db.QueryRowContext(ctx, `SELECT title, column_key, ext_key FROM item WHERE id=?`, id).
			Scan(&ref.Title, &ref.Column, &extKey)
		ref.ExtKey = extKey.String
		return ref
	}

	// Collect link endpoints first (drain and close cursors before any refFor).
	type edge struct{ id, kind string }
	var outgoing, incoming []edge
	if err := func() error {
		rows, err := s.db.QueryContext(ctx, `SELECT to_item, kind FROM link WHERE from_item=?`, itemID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var e edge
			if err := rows.Scan(&e.id, &e.kind); err != nil {
				return err
			}
			outgoing = append(outgoing, e)
		}
		return rows.Err()
	}(); err != nil {
		return board.ItemDetail{}, err
	}
	if err := func() error {
		rows, err := s.db.QueryContext(ctx, `SELECT from_item, kind FROM link WHERE to_item=?`, itemID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var e edge
			if err := rows.Scan(&e.id, &e.kind); err != nil {
				return err
			}
			incoming = append(incoming, e)
		}
		return rows.Err()
	}(); err != nil {
		return board.ItemDetail{}, err
	}

	for _, e := range outgoing {
		switch e.kind {
		case "depends_on":
			d.DependsOn = append(d.DependsOn, refFor(e.id))
		case "related", "discovered_from":
			d.Related = append(d.Related, refFor(e.id))
		}
	}
	for _, e := range incoming {
		if e.kind == "depends_on" {
			d.DependedBy = append(d.DependedBy, refFor(e.id))
		}
	}
	return d, nil
}

// Links returns all dependency links for a plan.
func (s *Store) Links(ctx context.Context, planID string) ([]board.Link, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT from_item, to_item, kind FROM link WHERE plan_id=?`, planID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []board.Link
	for rows.Next() {
		var l board.Link
		if err := rows.Scan(&l.From, &l.To, &l.Kind); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// AddLink records a dependency (idempotent). Silently ignores a link whose
// endpoints don't both exist yet.
func (s *Store) AddLink(ctx context.Context, planID, fromID, toID, kind string) error {
	if fromID == "" || toID == "" || fromID == toID {
		return nil
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO link(plan_id, from_item, to_item, kind) VALUES(?,?,?,?)`,
		planID, fromID, toID, kind)
	return err
}

// Profile reconstructs the active board.Profile for a plan from persisted state
// (columns from column_def, policies + lane dimension from the plan row).
func (s *Store) Profile(ctx context.Context, planID string) (board.Profile, error) {
	var prof board.Profile
	var policiesJSON string
	err := s.db.QueryRowContext(ctx,
		`SELECT profile, lane_dim, policies FROM plan WHERE id=?`, planID).
		Scan(&prof.Key, &prof.LaneDimension, &policiesJSON)
	if err != nil {
		return prof, err
	}
	if err := json.Unmarshal([]byte(policiesJSON), &prof.Policies); err != nil {
		return prof, fmt.Errorf("decode policies: %w", err)
	}
	cols, err := s.Columns(ctx, planID)
	if err != nil {
		return prof, err
	}
	prof.Columns = cols
	return prof, nil
}

// Columns returns the plan's columns ordered left-to-right.
func (s *Store) Columns(ctx context.Context, planID string) ([]board.ColumnDef, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT key,name,position,is_done,wip_limit FROM column_def WHERE plan_id=? ORDER BY position`, planID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []board.ColumnDef
	for rows.Next() {
		var c board.ColumnDef
		var isDone int
		var wip sql.NullInt64
		if err := rows.Scan(&c.Key, &c.Name, &c.Position, &isDone, &wip); err != nil {
			return nil, err
		}
		c.IsDone = isDone == 1
		if wip.Valid {
			v := int(wip.Int64)
			c.WIPLimit = &v
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) columnKeys(ctx context.Context, planID string) (map[string]bool, error) {
	cols, err := s.Columns(ctx, planID)
	if err != nil {
		return nil, err
	}
	keys := make(map[string]bool, len(cols))
	for _, c := range cols {
		keys[c.Key] = true
	}
	return keys, nil
}

// EnsureLane creates a swim lane if absent (idempotent by key). Default use:
// one lane per agent.
func (s *Store) EnsureLane(ctx context.Context, planID string, l board.Lane) (board.Lane, error) {
	var exists int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(1) FROM lane WHERE plan_id=? AND key=?`, planID, l.Key).Scan(&exists); err != nil {
		return board.Lane{}, err
	}
	if exists > 0 {
		return l, nil
	}
	if l.Position == 0 {
		s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(position)+1,0) FROM lane WHERE plan_id=?`, planID).Scan(&l.Position)
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO lane(id,plan_id,key,name,agent_id,position) VALUES(?,?,?,?,?,?)`,
		newID(), planID, l.Key, l.Name, l.AgentID, l.Position)
	return l, err
}

// Lanes lists the plan's swim lanes.
func (s *Store) Lanes(ctx context.Context, planID string) ([]board.Lane, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT key,name,COALESCE(agent_id,''),position FROM lane WHERE plan_id=? ORDER BY position`, planID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []board.Lane
	for rows.Next() {
		var l board.Lane
		if err := rows.Scan(&l.Key, &l.Name, &l.AgentID, &l.Position); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// CreateItem inserts a work item, validating enums and labels.
func (s *Store) CreateItem(ctx context.Context, agentID string, it *board.Item) (*board.Item, error) {
	if !it.Kind.Valid() {
		return nil, fmt.Errorf("invalid kind %q", it.Kind)
	}
	if it.SpecStatus == "" {
		it.SpecStatus = board.SpecMissing
	}
	if !it.SpecStatus.Valid() {
		return nil, fmt.Errorf("invalid spec_status %q", it.SpecStatus)
	}
	if it.Priority == "" {
		it.Priority = board.PriorityP2
	}
	if !board.ValidPriority(it.Priority) {
		return nil, fmt.Errorf("invalid priority %q", it.Priority)
	}
	colKeys, err := s.columnKeys(ctx, it.PlanID)
	if err != nil {
		return nil, err
	}
	if it.ColumnKey == "" {
		it.ColumnKey = "backlog"
	}
	if !colKeys[it.ColumnKey] {
		return nil, fmt.Errorf("unknown column %q", it.ColumnKey)
	}
	for _, l := range it.Labels {
		if err := board.ValidateLabel(l, colKeys); err != nil {
			return nil, err
		}
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	ts := now()
	it.ID = newID()
	it.CreatedAt, it.UpdatedAt = ts, ts
	var parent, lane any
	if it.ParentID != "" {
		parent = it.ParentID
	}
	if it.LaneKey != "" {
		lane = it.LaneKey
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO item(id,plan_id,parent_id,kind,title,body,content,column_key,lane_key,spec_ref,spec_status,priority,blocked,blocked_reason,position,created_at,updated_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		it.ID, it.PlanID, parent, it.Kind, it.Title, it.Body, it.Content, it.ColumnKey, lane,
		it.SpecRef, it.SpecStatus, it.Priority, boolInt(it.Blocked), it.BlockedReason,
		it.Position, it.CreatedAt, it.UpdatedAt); err != nil {
		return nil, err
	}
	if err := replaceLabelsTx(ctx, tx, it.ID, it.Labels); err != nil {
		return nil, err
	}
	if err := logEventTx(ctx, tx, it.PlanID, it.ID, agentID, "created",
		fmt.Sprintf("%s %q in %s", it.Kind, it.Title, it.ColumnKey)); err != nil {
		return nil, err
	}
	if err := s.commit(tx); err != nil {
		return nil, err
	}
	return it, nil
}

// UpsertByExtKey inserts or updates an item keyed by (plan_id, ext_key). Used by
// the item_upsert tool (methodology hydration) so re-running only updates existing
// cards rather than duplicating them. It bypasses policy gates (the external source
// is authoritative for the item's state) but DOES validate labels — a mistagged
// placement label (view:/lane:/column:) would otherwise silently hide the card
// from every view. Labels are fully replaced.
func (s *Store) UpsertByExtKey(ctx context.Context, agentID string, it *board.Item) (*board.Item, error) {
	if it.ExtKey == "" {
		return nil, fmt.Errorf("UpsertByExtKey requires ext_key")
	}
	if !it.Kind.Valid() {
		return nil, fmt.Errorf("invalid kind %q", it.Kind)
	}
	if it.SpecStatus == "" {
		it.SpecStatus = board.SpecMissing
	}
	if it.Priority == "" {
		it.Priority = board.PriorityP2
	}
	if it.ColumnKey == "" {
		it.ColumnKey = "backlog"
	}
	colKeys, err := s.columnKeys(ctx, it.PlanID)
	if err != nil {
		return nil, err
	}
	if !colKeys[it.ColumnKey] {
		return nil, fmt.Errorf("unknown column %q", it.ColumnKey)
	}
	for _, l := range it.Labels {
		if err := board.ValidateLabel(l, colKeys); err != nil {
			return nil, err
		}
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var existingID string
	err = tx.QueryRowContext(ctx, `SELECT id FROM item WHERE plan_id=? AND ext_key=?`, it.PlanID, it.ExtKey).Scan(&existingID)
	ts := now()
	var parent, lane any
	if it.ParentID != "" {
		parent = it.ParentID
	}
	if it.LaneKey != "" {
		lane = it.LaneKey
	}

	switch err {
	case sql.ErrNoRows:
		it.ID = newID()
		it.CreatedAt, it.UpdatedAt = ts, ts
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO item(id,plan_id,parent_id,kind,title,body,content,column_key,lane_key,spec_ref,spec_status,priority,blocked,blocked_reason,position,ext_key,created_at,updated_at)
			 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			it.ID, it.PlanID, parent, it.Kind, it.Title, it.Body, it.Content, it.ColumnKey, lane,
			it.SpecRef, it.SpecStatus, it.Priority, boolInt(it.Blocked), it.BlockedReason,
			it.Position, it.ExtKey, it.CreatedAt, it.UpdatedAt); err != nil {
			return nil, err
		}
	case nil:
		it.ID = existingID
		it.UpdatedAt = ts
		if _, err := tx.ExecContext(ctx,
			`UPDATE item SET parent_id=?,kind=?,title=?,body=?,content=?,column_key=?,lane_key=?,spec_ref=?,spec_status=?,priority=?,blocked=?,blocked_reason=?,updated_at=? WHERE id=?`,
			parent, it.Kind, it.Title, it.Body, it.Content, it.ColumnKey, lane, it.SpecRef, it.SpecStatus,
			it.Priority, boolInt(it.Blocked), it.BlockedReason, it.UpdatedAt, it.ID); err != nil {
			return nil, err
		}
	default:
		return nil, err
	}

	if err := replaceLabelsTx(ctx, tx, it.ID, it.Labels); err != nil {
		return nil, err
	}
	if err := s.commit(tx); err != nil {
		return nil, err
	}
	return it, nil
}

// ItemIDByExtKey resolves an external key to an internal item id (for parenting).
func (s *Store) ItemIDByExtKey(ctx context.Context, planID, extKey string) (string, bool) {
	var id string
	err := s.db.QueryRowContext(ctx, `SELECT id FROM item WHERE plan_id=? AND ext_key=?`, planID, extKey).Scan(&id)
	if err != nil {
		return "", false
	}
	return id, true
}

// MoveItem changes an item's column (and optionally lane), enforcing WIP limits.
func (s *Store) MoveItem(ctx context.Context, agentID, itemID, toColumn, toLane string) error {
	planID, err := s.itemPlan(ctx, itemID)
	if err != nil {
		return err
	}
	colKeys, err := s.columnKeys(ctx, planID)
	if err != nil {
		return err
	}
	if !colKeys[toColumn] {
		return fmt.Errorf("unknown column %q", toColumn)
	}

	// Policy engine: the active methodology profile decides whether this move is
	// legal — this is where the workflow and the swim lanes actually interact.
	prof, err := s.Profile(ctx, planID)
	if err != nil {
		return err
	}
	cur, err := s.getItem(ctx, itemID)
	if err != nil {
		return err
	}
	if err := prof.CheckLeave(*cur, toColumn); err != nil {
		return err
	}

	destLane := cur.LaneKey
	if toLane != "" {
		destLane = toLane
	}
	if err := s.checkWIP(ctx, planID, toColumn); err != nil {
		return err
	}
	if err := s.checkLaneWIP(ctx, prof, planID, destLane, itemID); err != nil {
		return err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if toLane != "" {
		_, err = tx.ExecContext(ctx,
			`UPDATE item SET column_key=?, lane_key=?, updated_at=? WHERE id=?`,
			toColumn, toLane, now(), itemID)
	} else {
		_, err = tx.ExecContext(ctx,
			`UPDATE item SET column_key=?, updated_at=? WHERE id=?`,
			toColumn, now(), itemID)
	}
	if err != nil {
		return err
	}
	if err := logEventTx(ctx, tx, planID, itemID, agentID, "moved", "-> "+toColumn); err != nil {
		return err
	}
	return s.commit(tx)
}

func (s *Store) checkWIP(ctx context.Context, planID, columnKey string) error {
	var limit sql.NullInt64
	err := s.db.QueryRowContext(ctx,
		`SELECT wip_limit FROM column_def WHERE plan_id=? AND key=?`, planID, columnKey).Scan(&limit)
	if err != nil || !limit.Valid {
		return nil
	}
	var count int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(1) FROM item WHERE plan_id=? AND column_key=?`, planID, columnKey).Scan(&count); err != nil {
		return err
	}
	if int64(count) >= limit.Int64 {
		return fmt.Errorf("WIP limit reached for column %q (%d)", columnKey, limit.Int64)
	}
	return nil
}

// checkLaneWIP enforces the profile's per-lane WIP cap. excludeItemID is skipped
// from the count so re-moving an item already in the lane isn't double-counted.
func (s *Store) checkLaneWIP(ctx context.Context, prof board.Profile, planID, laneKey, excludeItemID string) error {
	limit, ok := prof.LaneWIPLimit(laneKey)
	if !ok {
		return nil
	}
	var count int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(1) FROM item WHERE plan_id=? AND lane_key=? AND id<>?`,
		planID, laneKey, excludeItemID).Scan(&count); err != nil {
		return err
	}
	if count >= limit {
		return fmt.Errorf("lane WIP limit reached for lane %q (%d)", laneKey, limit)
	}
	return nil
}

// getItem loads a single item (without labels) for policy checks.
func (s *Store) getItem(ctx context.Context, itemID string) (*board.Item, error) {
	var it board.Item
	var blocked int
	err := s.db.QueryRowContext(ctx,
		`SELECT id,plan_id,COALESCE(parent_id,''),kind,title,body,content,column_key,COALESCE(lane_key,''),
		        spec_ref,spec_status,priority,blocked,blocked_reason,position,ext_key,created_at,updated_at
		 FROM item WHERE id=?`, itemID).
		Scan(&it.ID, &it.PlanID, &it.ParentID, &it.Kind, &it.Title, &it.Body, &it.Content,
			&it.ColumnKey, &it.LaneKey, &it.SpecRef, &it.SpecStatus, &it.Priority,
			&blocked, &it.BlockedReason, &it.Position, &it.ExtKey, &it.CreatedAt, &it.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("item %q not found", itemID)
	}
	if err != nil {
		return nil, err
	}
	it.Blocked = blocked == 1
	return &it, nil
}

// SetBlocked toggles the blocked flag with a reason.
func (s *Store) SetBlocked(ctx context.Context, agentID, itemID string, blocked bool, reason string) error {
	planID, err := s.itemPlan(ctx, itemID)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx,
		`UPDATE item SET blocked=?, blocked_reason=?, updated_at=? WHERE id=?`,
		boolInt(blocked), reason, now(), itemID); err != nil {
		return err
	}
	kind := "unblocked"
	if blocked {
		kind = "blocked"
	}
	if err := logEventTx(ctx, tx, planID, itemID, agentID, kind, reason); err != nil {
		return err
	}
	return s.commit(tx)
}

// SetSpec updates the spec reference/status (the SDD heartbeat).
func (s *Store) SetSpec(ctx context.Context, agentID, itemID, specRef string, status board.SpecStatus) error {
	if !status.Valid() {
		return fmt.Errorf("invalid spec_status %q", status)
	}
	planID, err := s.itemPlan(ctx, itemID)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx,
		`UPDATE item SET spec_ref=?, spec_status=?, updated_at=? WHERE id=?`,
		specRef, status, now(), itemID); err != nil {
		return err
	}
	if err := logEventTx(ctx, tx, planID, itemID, agentID, "spec",
		fmt.Sprintf("%s (%s)", specRef, status)); err != nil {
		return err
	}
	return s.commit(tx)
}

// SetContent replaces an item's content (the doc markdown a doc view renders). The
// agent supplies it — the server never reads files.
func (s *Store) SetContent(ctx context.Context, agentID, itemID, content string) error {
	planID, err := s.itemPlan(ctx, itemID)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx,
		`UPDATE item SET content=?, updated_at=? WHERE id=?`, content, now(), itemID); err != nil {
		return err
	}
	if err := logEventTx(ctx, tx, planID, itemID, agentID, "content", fmt.Sprintf("%d bytes", len(content))); err != nil {
		return err
	}
	return s.commit(tx)
}

// AddLabels validates and adds labels to an item (idempotent).
func (s *Store) AddLabels(ctx context.Context, agentID, itemID string, labels []board.Label) error {
	planID, err := s.itemPlan(ctx, itemID)
	if err != nil {
		return err
	}
	colKeys, err := s.columnKeys(ctx, planID)
	if err != nil {
		return err
	}
	for _, l := range labels {
		if err := board.ValidateLabel(l, colKeys); err != nil {
			return err
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, l := range labels {
		if _, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO label(item_id,ns,value) VALUES(?,?,?)`,
			itemID, l.NS, l.Value); err != nil {
			return err
		}
	}
	if err := logEventTx(ctx, tx, planID, itemID, agentID, "labeled", labelsString(labels)); err != nil {
		return err
	}
	return s.commit(tx)
}

// AddComment appends a comment event to an item.
func (s *Store) AddComment(ctx context.Context, agentID, itemID, text string) error {
	planID, err := s.itemPlan(ctx, itemID)
	if err != nil {
		return err
	}
	return s.logEvent(ctx, planID, itemID, agentID, "commented", text)
}

// ListItems returns items for a plan, optionally filtered.
type Filter struct {
	ColumnKey string
	LaneKey   string
	ParentID  string
	Kind      string
	Blocked   *bool
}

func (s *Store) ListItems(ctx context.Context, planID string, f Filter) ([]board.Item, error) {
	q := `SELECT id,plan_id,COALESCE(parent_id,''),kind,title,body,content,column_key,COALESCE(lane_key,''),
	             spec_ref,spec_status,priority,blocked,blocked_reason,position,ext_key,created_at,updated_at
	      FROM item WHERE plan_id=?`
	args := []any{planID}
	if f.ColumnKey != "" {
		q += " AND column_key=?"
		args = append(args, f.ColumnKey)
	}
	if f.LaneKey != "" {
		q += " AND lane_key=?"
		args = append(args, f.LaneKey)
	}
	if f.ParentID != "" {
		q += " AND parent_id=?"
		args = append(args, f.ParentID)
	}
	if f.Kind != "" {
		q += " AND kind=?"
		args = append(args, f.Kind)
	}
	if f.Blocked != nil {
		q += " AND blocked=?"
		args = append(args, boolInt(*f.Blocked))
	}
	q += " ORDER BY position, created_at"

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []board.Item
	for rows.Next() {
		var it board.Item
		var blocked int
		if err := rows.Scan(&it.ID, &it.PlanID, &it.ParentID, &it.Kind, &it.Title, &it.Body, &it.Content,
			&it.ColumnKey, &it.LaneKey, &it.SpecRef, &it.SpecStatus, &it.Priority,
			&blocked, &it.BlockedReason, &it.Position, &it.ExtKey, &it.CreatedAt, &it.UpdatedAt); err != nil {
			return nil, err
		}
		it.Blocked = blocked == 1
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range items {
		labels, err := s.itemLabels(ctx, items[i].ID)
		if err != nil {
			return nil, err
		}
		items[i].Labels = labels
	}
	return items, nil
}

func (s *Store) itemLabels(ctx context.Context, itemID string) ([]board.Label, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT ns,value FROM label WHERE item_id=? ORDER BY ns,value`, itemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []board.Label
	for rows.Next() {
		var l board.Label
		if err := rows.Scan(&l.NS, &l.Value); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// ItemPlanID resolves an item to the board (plan) it belongs to. The bool is
// false if the item doesn't exist — used to validate cross-item operations like
// linking, where both endpoints must live on the same board.
func (s *Store) ItemPlanID(ctx context.Context, itemID string) (string, bool) {
	planID, err := s.itemPlan(ctx, itemID)
	if err != nil {
		return "", false
	}
	return planID, true
}

func (s *Store) itemPlan(ctx context.Context, itemID string) (string, error) {
	var planID string
	err := s.db.QueryRowContext(ctx, `SELECT plan_id FROM item WHERE id=?`, itemID).Scan(&planID)
	if err == sql.ErrNoRows {
		return "", fmt.Errorf("item %q not found", itemID)
	}
	return planID, err
}

func (s *Store) logEvent(ctx context.Context, planID, itemID, agentID, kind, detail string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO event(id,plan_id,item_id,agent_id,kind,detail,at) VALUES(?,?,?,?,?,?,?)`,
		newID(), planID, nullable(itemID), agentID, kind, detail, now())
	return err
}

// --- helpers ---

func replaceLabelsTx(ctx context.Context, tx *sql.Tx, itemID string, labels []board.Label) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM label WHERE item_id=?`, itemID); err != nil {
		return err
	}
	for _, l := range labels {
		if _, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO label(item_id,ns,value) VALUES(?,?,?)`, itemID, l.NS, l.Value); err != nil {
			return err
		}
	}
	return nil
}

func logEventTx(ctx context.Context, tx *sql.Tx, planID, itemID, agentID, kind, detail string) error {
	_, err := tx.ExecContext(ctx,
		`INSERT INTO event(id,plan_id,item_id,agent_id,kind,detail,at) VALUES(?,?,?,?,?,?,?)`,
		newID(), planID, nullable(itemID), agentID, kind, detail, now())
	return err
}

func labelsString(labels []board.Label) string {
	s := ""
	for i, l := range labels {
		if i > 0 {
			s += ", "
		}
		s += l.String()
	}
	return s
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}
