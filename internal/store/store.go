// Package store is the SQLite persistence layer for kanban-mcp.
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

	"github.com/ethanhinson/kanban-mcp/internal/board"
	"github.com/oklog/ulid/v2"
	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schemaSQL string

// Store wraps the SQLite connection.
type Store struct {
	db *sql.DB
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
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func now() string    { return time.Now().UTC().Format(time.RFC3339) }
func newID() string  { return ulid.Make().String() }

// EnsurePlan returns the single plan for this db. If the db is empty it creates
// the plan from the named methodology profile (built-in preset), seeding that
// profile's columns, seed lanes, lane dimension, and enforcement policies. The
// profile is what binds meaning to both board axes; see board.Profile.
// An unknown profileKey falls back to "sdd".
func (s *Store) EnsurePlan(ctx context.Context, name, description, profileKey string) (*board.Plan, error) {
	var p board.Plan
	row := s.db.QueryRowContext(ctx,
		`SELECT id, name, description, profile, lane_dim, policies, created_at, updated_at FROM plan LIMIT 1`)
	err := row.Scan(&p.ID, &p.Name, &p.Description, &p.ProfileKey, &p.LaneDimension, &p.PoliciesJSON, &p.CreatedAt, &p.UpdatedAt)
	if err == nil {
		return &p, nil // existing plan keeps its profile; profileKey arg is ignored
	}
	if err != sql.ErrNoRows {
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
	p = board.Plan{
		ID: newID(), Name: name, Description: description,
		ProfileKey: prof.Key, LaneDimension: string(prof.LaneDimension), PoliciesJSON: string(policiesJSON),
		CreatedAt: ts, UpdatedAt: ts,
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO plan(id,name,description,profile,lane_dim,policies,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?)`,
		p.ID, p.Name, p.Description, p.ProfileKey, p.LaneDimension, p.PoliciesJSON, p.CreatedAt, p.UpdatedAt); err != nil {
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

// LoadPlan returns the single plan row with all fields populated.
func (s *Store) LoadPlan(ctx context.Context, planID string) (*board.Plan, error) {
	var p board.Plan
	err := s.db.QueryRowContext(ctx,
		`SELECT id,name,description,profile,lane_dim,policies,created_at,updated_at FROM plan WHERE id=?`, planID).
		Scan(&p.ID, &p.Name, &p.Description, &p.ProfileKey, &p.LaneDimension, &p.PoliciesJSON, &p.CreatedAt, &p.UpdatedAt)
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
	return board.BuildSnapshot(*plan, cols, lanes, items), nil
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
		`INSERT INTO item(id,plan_id,parent_id,kind,title,body,column_key,lane_key,spec_ref,spec_status,priority,blocked,blocked_reason,position,created_at,updated_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		it.ID, it.PlanID, parent, it.Kind, it.Title, it.Body, it.ColumnKey, lane,
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
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return it, nil
}

// UpsertByExtKey inserts or updates an item keyed by (plan_id, ext_key). Used by
// importers (e.g. docket) so re-running a sync updates existing cards in place
// rather than duplicating them. It bypasses policy gates because the external
// source is authoritative for the item's state. Labels are fully replaced.
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
			`INSERT INTO item(id,plan_id,parent_id,kind,title,body,column_key,lane_key,spec_ref,spec_status,priority,blocked,blocked_reason,position,ext_key,created_at,updated_at)
			 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			it.ID, it.PlanID, parent, it.Kind, it.Title, it.Body, it.ColumnKey, lane,
			it.SpecRef, it.SpecStatus, it.Priority, boolInt(it.Blocked), it.BlockedReason,
			it.Position, it.ExtKey, it.CreatedAt, it.UpdatedAt); err != nil {
			return nil, err
		}
	case nil:
		it.ID = existingID
		it.UpdatedAt = ts
		if _, err := tx.ExecContext(ctx,
			`UPDATE item SET parent_id=?,kind=?,title=?,body=?,column_key=?,lane_key=?,spec_ref=?,spec_status=?,priority=?,blocked=?,blocked_reason=?,updated_at=? WHERE id=?`,
			parent, it.Kind, it.Title, it.Body, it.ColumnKey, lane, it.SpecRef, it.SpecStatus,
			it.Priority, boolInt(it.Blocked), it.BlockedReason, it.UpdatedAt, it.ID); err != nil {
			return nil, err
		}
	default:
		return nil, err
	}

	if err := replaceLabelsTx(ctx, tx, it.ID, it.Labels); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
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
	return tx.Commit()
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
		`SELECT id,plan_id,COALESCE(parent_id,''),kind,title,body,column_key,COALESCE(lane_key,''),
		        spec_ref,spec_status,priority,blocked,blocked_reason,position,ext_key,created_at,updated_at
		 FROM item WHERE id=?`, itemID).
		Scan(&it.ID, &it.PlanID, &it.ParentID, &it.Kind, &it.Title, &it.Body,
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
	return tx.Commit()
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
	return tx.Commit()
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
	return tx.Commit()
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
	q := `SELECT id,plan_id,COALESCE(parent_id,''),kind,title,body,column_key,COALESCE(lane_key,''),
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
		if err := rows.Scan(&it.ID, &it.PlanID, &it.ParentID, &it.Kind, &it.Title, &it.Body,
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
