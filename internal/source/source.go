// Package source is the decoupled-input seam for the board. The board process
// owns the agent's LIVE in-flight work (items created via board_start /
// item_create). Everything else — backlog, done, ADRs — is a read-only projection
// from an external source of truth (docket today; GitHub issues, files, etc.
// later). A Provider fetches that external data harness-agnostically; Sync
// projects it onto a board idempotently.
//
// This is deliberately separate from the board lifecycle: WHEN and WHETHER a
// source refreshes is decoupled from the running board process. An agent (or a
// cron, or a CLI flag) triggers Sync; the board itself never owns the source.
package source

import (
	"context"
	"fmt"

	"github.com/ethanhinson/kanban-mcp/internal/board"
	"github.com/ethanhinson/kanban-mcp/internal/store"
)

// Config carries provider parameters. Fields are provider-specific; a provider
// reads the ones it needs and ignores the rest.
type Config struct {
	DocsDir string // docket: path to the docs dir (e.g. <repo>/.docket/docs)
}

// Section marks what part of the board an external item projects into. It mirrors
// the three decoupled views: backlog and done are read-only projections; ADRs are
// reference records surfaced alongside the work.
type Section string

const (
	SectionBacklog Section = "backlog"
	SectionDone    Section = "done"
	SectionADR     Section = "adr"
)

// ExternalItem is one record from a source, in board terms. ExtKey is the stable
// idempotency key (e.g. "docket:74", "adr:5"): re-syncing upserts by it rather
// than duplicating. Lane is created on demand if absent.
type ExternalItem struct {
	ExtKey     string
	Kind       board.Kind
	Title      string
	Body       string
	Column     string
	Lane       string
	SpecRef    string
	SpecStatus board.SpecStatus
	Priority   string
	Blocked    bool
	BlockedReason string
	Labels     []board.Label
	Section    Section
}

// ExternalLink is a dependency between two external items, addressed by their
// ext keys (resolved to internal ids during Sync, after all items exist).
type ExternalLink struct {
	FromExtKey string
	ToExtKey   string
	Kind       string // depends_on | related | discovered_from
}

// Provider reads an external source of truth and returns it in board terms. It is
// pure I/O against the source — it does not touch the store (Sync does that), so
// providers are trivially testable and store-agnostic.
type Provider interface {
	Kind() string
	Fetch(ctx context.Context) ([]ExternalItem, []ExternalLink, error)
}

// NewProvider builds a provider by kind. "docket" is the only impl today; the
// factory is the extension point for GitHub issues, files, etc.
func NewProvider(kind string, cfg Config) (Provider, error) {
	switch kind {
	case "", "docket":
		if cfg.DocsDir == "" {
			return nil, fmt.Errorf("docket source requires docs_dir")
		}
		return &docketProvider{docsDir: cfg.DocsDir}, nil
	default:
		return nil, fmt.Errorf("unknown source %q (known: docket)", kind)
	}
}

// Result summarizes a Sync run.
type Result struct {
	Items int
	Links int
}

// Sync projects a provider's external items onto a board idempotently: ensure a
// lane exists for each item's lane, upsert every item by ext key, then record
// links once all endpoints exist. Upserts go through the store's ext-key path,
// which bypasses policy gates because the external source is authoritative for
// projected state — the board's own gates govern the agent's live items only.
func Sync(ctx context.Context, st *store.Store, planID string, p Provider) (Result, error) {
	items, links, err := p.Fetch(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("%s fetch: %w", p.Kind(), err)
	}

	// Pass 1: ensure a lane per distinct non-shared lane key.
	seen := map[string]bool{}
	for _, it := range items {
		lane := it.Lane
		if lane == "" || lane == "shared" || seen[lane] {
			continue
		}
		seen[lane] = true
		if _, err := st.EnsureLane(ctx, planID, board.Lane{Key: lane, Name: lane}); err != nil {
			return Result{}, fmt.Errorf("ensure lane %q: %w", lane, err)
		}
	}

	// Pass 2: upsert every item so ext keys resolve for the link pass.
	agent := p.Kind() + "-sync"
	for _, it := range items {
		bi := &board.Item{
			PlanID:        planID,
			Kind:          it.Kind,
			Title:         it.Title,
			Body:          it.Body,
			ColumnKey:     it.Column,
			LaneKey:       it.Lane,
			SpecRef:       it.SpecRef,
			SpecStatus:    it.SpecStatus,
			Priority:      it.Priority,
			Blocked:       it.Blocked,
			BlockedReason: it.BlockedReason,
			ExtKey:        it.ExtKey,
			Labels:        it.Labels,
		}
		if _, err := st.UpsertByExtKey(ctx, agent, bi); err != nil {
			return Result{}, fmt.Errorf("upsert %s: %w", it.ExtKey, err)
		}
	}

	// Pass 3: record links now that every card exists.
	linkCount := 0
	for _, l := range links {
		from, ok := st.ItemIDByExtKey(ctx, planID, l.FromExtKey)
		if !ok {
			continue
		}
		to, ok := st.ItemIDByExtKey(ctx, planID, l.ToExtKey)
		if !ok {
			continue
		}
		if err := st.AddLink(ctx, planID, from, to, l.Kind); err == nil {
			linkCount++
		}
	}

	return Result{Items: len(items), Links: linkCount}, nil
}
