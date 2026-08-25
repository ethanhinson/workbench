-- kanban-mcp schema. One SQLite file hosts MANY boards (plan rows); an agent
-- creates/selects a board at runtime via board_start. Every table below is keyed
-- by plan_id, so a database file is a multi-board container.
-- Nested model: plan > epic > story > task. Boards are views over a plan.

PRAGMA journal_mode = WAL;      -- concurrent agents: readers don't block the writer
PRAGMA foreign_keys = ON;
PRAGMA busy_timeout = 5000;     -- wait rather than fail when another agent holds the write lock

-- Boards. Each row is one board (plan) in this database. A board belongs to a
-- project (by default a directory path); board names are unique WITHIN a project,
-- so two projects can each have a board named "auth work". board_start is
-- idempotent by (project, name) — starting an existing board selects it.
CREATE TABLE IF NOT EXISTS plan (
    id          TEXT PRIMARY KEY,          -- ulid
    name        TEXT NOT NULL,
    project     TEXT NOT NULL DEFAULT '',  -- owning project (a directory path by default)
    description TEXT NOT NULL DEFAULT '',
    profile     TEXT NOT NULL DEFAULT 'sdd', -- active methodology profile (sdd|scrum|kanban|docket|openspec|superpowers|custom)
    lane_dim    TEXT NOT NULL DEFAULT 'agent', -- what a swim lane means under this profile
    policies    TEXT NOT NULL DEFAULT '{}',  -- JSON-encoded board.Policies (the enforcement rules)
    layout      TEXT NOT NULL DEFAULT '',    -- JSON-encoded board.Layout (agent-authored UI); '' = no layout
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL,
    UNIQUE (project, name)              -- board names are unique WITHIN a project
);

-- Workflow columns (stages). Seeded with SDD-opinionated defaults, overridable.
CREATE TABLE IF NOT EXISTS column_def (
    id       TEXT PRIMARY KEY,
    plan_id  TEXT NOT NULL REFERENCES plan(id) ON DELETE CASCADE,
    key      TEXT NOT NULL,                 -- stable machine key, e.g. "in_progress"
    name     TEXT NOT NULL,                 -- display name, e.g. "In Progress"
    position INTEGER NOT NULL,              -- left-to-right order
    is_done  INTEGER NOT NULL DEFAULT 0,    -- terminal column?
    wip_limit INTEGER,                      -- optional work-in-progress cap
    UNIQUE (plan_id, key)
);

-- Swim lanes. Configurable; default is one lane per agent. Lanes roll up into the plan.
CREATE TABLE IF NOT EXISTS lane (
    id         TEXT PRIMARY KEY,
    plan_id    TEXT NOT NULL REFERENCES plan(id) ON DELETE CASCADE,
    key        TEXT NOT NULL,               -- e.g. agent id / owner slug
    name       TEXT NOT NULL,
    agent_id   TEXT,                        -- owning agent, nullable for shared lanes
    position   INTEGER NOT NULL,
    UNIQUE (plan_id, key)
);

-- Work items. Nested via parent_id + kind (epic > story > task).
CREATE TABLE IF NOT EXISTS item (
    id          TEXT PRIMARY KEY,
    plan_id     TEXT NOT NULL REFERENCES plan(id) ON DELETE CASCADE,
    parent_id   TEXT REFERENCES item(id) ON DELETE CASCADE,
    kind        TEXT NOT NULL,              -- epic | story | task | bug | spike
    title       TEXT NOT NULL,
    body        TEXT NOT NULL DEFAULT '',   -- short free-text notes
    content     TEXT NOT NULL DEFAULT '',   -- full doc markdown (spec/ADR/notes) the agent supplied; rendered by a doc view
    column_key  TEXT NOT NULL,              -- current stage
    lane_key    TEXT,                       -- owning swim lane (nullable = unassigned)
    spec_ref    TEXT NOT NULL DEFAULT '',   -- path/URL to the spec doc (SDD)
    spec_status TEXT NOT NULL DEFAULT 'missing', -- missing | draft | approved
    priority    TEXT NOT NULL DEFAULT 'p2', -- p0 | p1 | p2 | p3
    blocked     INTEGER NOT NULL DEFAULT 0, -- blocked flag (orthogonal to column)
    blocked_reason TEXT NOT NULL DEFAULT '',
    position    INTEGER NOT NULL DEFAULT 0, -- order within (column, lane)
    ext_key     TEXT NOT NULL DEFAULT '',   -- external source id for idempotent import (e.g. "docket:74")
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_item_extkey ON item(plan_id, ext_key) WHERE ext_key <> '';

CREATE INDEX IF NOT EXISTS idx_item_plan   ON item(plan_id);
CREATE INDEX IF NOT EXISTS idx_item_parent ON item(parent_id);
CREATE INDEX IF NOT EXISTS idx_item_board  ON item(plan_id, column_key, lane_key);

-- Namespaced labels (type:/stage:/priority:/agent:/spec:). Enum-validated in code
-- so agents can't drift the taxonomy. Stored normalized as "ns:value".
CREATE TABLE IF NOT EXISTS label (
    item_id TEXT NOT NULL REFERENCES item(id) ON DELETE CASCADE,
    ns      TEXT NOT NULL,                  -- namespace, e.g. "priority"
    value   TEXT NOT NULL,                  -- e.g. "p0"
    PRIMARY KEY (item_id, ns, value)
);

-- First-class dependency links between items (flat board shows these instead of
-- containment). kind: depends_on | related | discovered_from. Direction: from
-- depends on / relates to / was discovered from `to`.
CREATE TABLE IF NOT EXISTS link (
    plan_id   TEXT NOT NULL REFERENCES plan(id) ON DELETE CASCADE,
    from_item TEXT NOT NULL REFERENCES item(id) ON DELETE CASCADE,
    to_item   TEXT NOT NULL REFERENCES item(id) ON DELETE CASCADE,
    kind      TEXT NOT NULL,
    PRIMARY KEY (from_item, to_item, kind)
);
CREATE INDEX IF NOT EXISTS idx_link_from ON link(from_item);
CREATE INDEX IF NOT EXISTS idx_link_to   ON link(to_item);

-- Append-only activity log per item, so an agent can reconstruct "what's discussed".
CREATE TABLE IF NOT EXISTS event (
    id        TEXT PRIMARY KEY,
    plan_id   TEXT NOT NULL REFERENCES plan(id) ON DELETE CASCADE,
    item_id   TEXT REFERENCES item(id) ON DELETE CASCADE,
    agent_id  TEXT NOT NULL DEFAULT '',
    kind      TEXT NOT NULL,                -- created | moved | labeled | commented | blocked | ...
    detail    TEXT NOT NULL DEFAULT '',
    at        TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_event_item ON event(item_id, at);
