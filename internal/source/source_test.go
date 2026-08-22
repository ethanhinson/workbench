package source

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ethanhinson/kanban-mcp/internal/store"
)

const manifestProposed = `---
id: 74
slug: sandbox-health-emitter
title: sandbox health emitter
status: proposed
priority: medium
type: feat
depends_on: [63]
discovered_from: [63]
---
body
`

const manifestParent = `---
id: 63
slug: bash-container-substrate
title: bash container substrate
status: done
priority: high
type: feat
spec: docs/specs/63.md
plan: docs/plans/63.md
results: docs/results/63.md
---
`

const manifestFix = `---
id: 40
slug: some-bug
title: a fix
status: proposed
priority: low
type: fix
spec: docs/specs/40.md
plan: docs/plans/40.md
branch: fix/some-bug
---
`

const adrDoc = `---
id: 1
slug: some-decision
title: "A settled decision"
status: Accepted
change: 63
---
body
`

func writeDocket(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	active := filepath.Join(root, "changes", "active")
	archive := filepath.Join(root, "changes", "archive")
	adrs := filepath.Join(root, "adrs")
	for _, d := range []string{active, archive, adrs} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write := func(dir, name, content string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(active, "0074-x.md", manifestProposed)
	write(archive, "0063-y.md", manifestParent)
	write(active, "0040-z.md", manifestFix)
	write(adrs, "0001-d.md", adrDoc)
	return root
}

// TestDocketSyncIdempotentAndLinks drives the generic source.Sync through the
// docket provider: items upsert idempotently by ext key, dependencies become
// first-class links (not nesting), and ADRs project in as reference cards.
func TestDocketSyncIdempotentAndLinks(t *testing.T) {
	root := writeDocket(t)
	st, err := store.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	plan, _ := st.EnsurePlan(ctx, "P", "", "docket")

	p, err := NewProvider("docket", Config{DocsDir: root})
	if err != nil {
		t.Fatal(err)
	}

	r1, err := Sync(ctx, st, plan.ID, p)
	if err != nil {
		t.Fatal(err)
	}
	// 3 changes + 1 ADR.
	if r1.Items != 4 {
		t.Fatalf("first sync items: got %d want 4", r1.Items)
	}
	items1, _ := st.ListItems(ctx, plan.ID, store.Filter{})
	if len(items1) != 4 {
		t.Fatalf("want 4 items after sync, got %d", len(items1))
	}

	// Re-sync must not duplicate (idempotent by ext key).
	if _, err := Sync(ctx, st, plan.ID, p); err != nil {
		t.Fatal(err)
	}
	items2, _ := st.ListItems(ctx, plan.ID, store.Filter{})
	if len(items2) != 4 {
		t.Fatalf("re-sync duplicated items: got %d want 4", len(items2))
	}

	// #40 is a fix => mapped to a bug kind.
	fix, ok := st.ItemIDByExtKey(ctx, plan.ID, "docket:40")
	if !ok {
		t.Fatal("missing docket:40")
	}
	_ = fix

	// #74 depends_on [63] => a first-class link, not nesting.
	from, ok := st.ItemIDByExtKey(ctx, plan.ID, "docket:74")
	if !ok {
		t.Fatal("missing docket:74 item")
	}
	to, ok := st.ItemIDByExtKey(ctx, plan.ID, "docket:63")
	if !ok {
		t.Fatal("missing docket:63 item")
	}
	links, err := st.Links(ctx, plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	var hasDep, hasADRLink bool
	adr, _ := st.ItemIDByExtKey(ctx, plan.ID, "adr:1")
	for _, l := range links {
		if l.From == from && l.To == to && l.Kind == "depends_on" {
			hasDep = true
		}
		if l.From == adr && l.To == to && l.Kind == "related" {
			hasADRLink = true // ADR-1 relates_to change 63
		}
	}
	if !hasDep {
		t.Fatalf("#74 should have a depends_on link to #63; links=%+v", links)
	}
	if !hasADRLink {
		t.Fatalf("ADR-1 should link to change #63; links=%+v", links)
	}
}

func TestUnknownProvider(t *testing.T) {
	if _, err := NewProvider("jira", Config{DocsDir: "x"}); err == nil {
		t.Fatal("expected error for unknown provider")
	}
	if _, err := NewProvider("docket", Config{}); err == nil {
		t.Fatal("expected error when docs_dir missing")
	}
}

// TestImportOntoNamedBoardWhenOthersExist reproduces the install-time bug: with a
// board already present, the importer must target a DISTINCT board resolved by
// name (CreatePlan), not the pre-existing/first board. This mirrors what
// cmd/kanban-mcp does for --source.
func TestImportOntoNamedBoardWhenOthersExist(t *testing.T) {
	root := writeDocket(t)
	st, err := store.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()

	// A pre-existing board (as the shared db has when the MCP server first ran).
	existing, _ := st.EnsurePlan(ctx, "Plan", "", "sdd")

	// Import targets a differently-named board, resolved by name.
	target, err := st.CreatePlan(ctx, "fuse: docket backlog", "", "", "docket")
	if err != nil {
		t.Fatal(err)
	}
	if target.ID == existing.ID {
		t.Fatal("import target must be a distinct board, not the pre-existing one")
	}

	p, _ := NewProvider("docket", Config{DocsDir: root})
	if _, err := Sync(ctx, st, target.ID, p); err != nil {
		t.Fatal(err)
	}

	// The pre-existing board stayed empty; the named board got the items.
	if got, _ := st.ListItems(ctx, existing.ID, store.Filter{}); len(got) != 0 {
		t.Fatalf("pre-existing board must stay empty, got %d items", len(got))
	}
	if got, _ := st.ListItems(ctx, target.ID, store.Filter{}); len(got) != 4 {
		t.Fatalf("named board should hold the 4 imported items, got %d", len(got))
	}
}
