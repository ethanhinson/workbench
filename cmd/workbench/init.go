package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/ethanhinson/workbench/internal/adapter"
	"github.com/ethanhinson/workbench/internal/store"
	"github.com/ethanhinson/workbench/internal/viz"
)

// runInit is `workbench init`: point at a repo, detect its methodology, create a
// project-scoped board, set the tool-idiomatic layout, hydrate it once from the
// on-disk source of truth, and print how to browse it. The board-onboarding slice
// of the init flow — one command from "git clone" to a populated board.
func runInit(args []string) {
	fs := flag.NewFlagSet("init", flag.ExitOnError)
	dbPath := fs.String("db", envOr("KANBAN_DB", "kanban.db"), "path to the SQLite board database")
	project := fs.String("project", envOr("KANBAN_PROJECT", ""), "repo root to onboard; empty => the working directory")
	httpAddr := fs.String("http", envOr("KANBAN_HTTP", ":7777"), "addr the server will serve the board on (for the printed URL)")
	name := fs.String("name", "", "board name; empty => \"<repo>: docket backlog\"")
	_ = fs.Parse(args)

	proj := *project
	if proj == "" {
		if cwd, err := os.Getwd(); err == nil {
			proj = cwd
		}
	}

	a, ok := adapter.Detect(proj)
	if !ok {
		fmt.Fprintf(os.Stderr, "workbench init: no docket footprint found under %s\n", proj)
		fmt.Fprintln(os.Stderr, "  (looked for .docket/docs/changes and docs/changes; openspec/superpowers detection is not wired yet)")
		os.Exit(1)
	}

	abs, err := filepath.Abs(*dbPath)
	if err != nil {
		log.Fatalf("resolve db path: %v", err)
	}
	st, err := store.Open(abs)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer st.Close()

	ctx := context.Background()
	boardName := *name
	if boardName == "" {
		boardName = docketBoardName(proj)
	}
	plan, err := st.CreatePlan(ctx, boardName, proj, "", a.Name())
	if err != nil {
		log.Fatalf("create board: %v", err)
	}
	if err := st.SetPlanLayout(ctx, plan.ID, adapter.DocketLayout()); err != nil {
		log.Fatalf("set layout: %v", err)
	}
	if err := a.Sync(ctx, st, plan.ID, proj); err != nil {
		log.Fatalf("sync: %v", err)
	}

	addr := viz.NormalizeAddr(*httpAddr)
	fmt.Printf("workbench: %q board ready (%s) — %d changes hydrated\n", a.Name(), boardName, boardItemCount(ctx, st, plan.ID))
	fmt.Printf("  browse:  %s\n", browseURL(addr))
	fmt.Printf("  serve:   workbench --db %s --project %s --http %s\n", abs, proj, addr)
}

func boardItemCount(ctx context.Context, st *store.Store, planID string) int {
	items, _ := st.ListItems(ctx, planID, store.Filter{})
	return len(items)
}
