package docket

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ethanhinson/kanban-mcp/internal/board"
)

const manifestProposed = `---
id: 74
slug: sandbox-health-emitter
title: sandbox health emitter
status: proposed
priority: medium
type: feat
spec:
plan:
depends_on: [63]
discovered_from: [63]
blocked_by:
trivial: false
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

func writeDocket(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	active := filepath.Join(root, "changes", "active")
	archive := filepath.Join(root, "changes", "archive")
	for _, d := range []string{active, archive} {
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
	return root
}

func TestImportMapping(t *testing.T) {
	changes, err := Import(writeDocket(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 3 {
		t.Fatalf("want 3 changes, got %d", len(changes))
	}
	byID := map[int]Change{}
	for _, c := range changes {
		byID[c.ID] = c
	}

	// #74 proposed, no spec/plan => backlog, needs-brainstorm, missing spec, feat lane.
	c74 := byID[74]
	if got := c74.ColumnFor(); got != "backlog" {
		t.Errorf("#74 column: got %q want backlog", got)
	}
	if got := c74.SpecStatusFor(); got != board.SpecMissing {
		t.Errorf("#74 spec: got %q", got)
	}
	if got := c74.PriorityFor(); got != board.PriorityP1 {
		t.Errorf("#74 priority: got %q", got)
	}
	if got := c74.LaneFor(); got != "feat" {
		t.Errorf("#74 lane: got %q", got)
	}

	// #63 done => done column, approved spec.
	if got := byID[63].ColumnFor(); got != "done" {
		t.Errorf("#63 column: got %q want done", got)
	}
	if got := byID[63].SpecStatusFor(); got != board.SpecApproved {
		t.Errorf("#63 spec: got %q want approved", got)
	}

	// #40 proposed + branch set => in_progress; fix type (=> bug kind in the source
	// mapping), fix lane.
	if got := byID[40].ColumnFor(); got != "in_progress" {
		t.Errorf("#40 column: got %q want in_progress", got)
	}
	if got := byID[40].Type; got != "fix" {
		t.Errorf("#40 type: got %q want fix", got)
	}
}

func TestImportADRs(t *testing.T) {
	root := t.TempDir()
	adrDir := filepath.Join(root, "adrs")
	if err := os.MkdirAll(adrDir, 0o755); err != nil {
		t.Fatal(err)
	}
	const adr = `---
id: 7
slug: scheduler-single-admission-authority
title: "Scheduler is the single admission authority"
status: Accepted
date: 2026-08-05
change: 8
relates_to: [3]
---
## Context
body
`
	if err := os.WriteFile(filepath.Join(adrDir, "0007-x.md"), []byte(adr), 0o644); err != nil {
		t.Fatal(err)
	}
	adrs, err := ImportADRs(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(adrs) != 1 {
		t.Fatalf("want 1 ADR, got %d", len(adrs))
	}
	a := adrs[0]
	if a.ID != 7 || a.Change != 8 || a.Status != "Accepted" {
		t.Errorf("ADR fields: %+v", a)
	}
	if a.Title != "Scheduler is the single admission authority" {
		t.Errorf("ADR title not unquoted: %q", a.Title)
	}
}
