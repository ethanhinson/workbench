// Command kanban-mcp is an MCP server exposing SDD-oriented kanban boards over
// stdio. One --db file hosts many boards, grouped by project (a directory path);
// the agent creates or selects a board at runtime with board_start, and each agent
// connects with its own --agent identity and swim lane.
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"path/filepath"

	"github.com/ethanhinson/kanban-mcp/internal/mcpserver"
	"github.com/ethanhinson/kanban-mcp/internal/source"
	"github.com/ethanhinson/kanban-mcp/internal/store"
	"github.com/ethanhinson/kanban-mcp/internal/viz"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	var (
		dbPath  = flag.String("db", envOr("KANBAN_DB", "kanban.db"), "path to the SQLite plan database")
		plan    = flag.String("plan", envOr("KANBAN_PLAN", ""), "board to focus: the viz default board and the --source import target (create-or-select by name). Empty => agents create boards at runtime via board_start")
		project = flag.String("project", envOr("KANBAN_PROJECT", ""), "default project (a directory path) new boards belong to; empty => the working directory")
		agent   = flag.String("agent", envOr("KANBAN_AGENT", "agent"), "calling agent id (its default swim lane)")
		profile = flag.String("profile", envOr("KANBAN_PROFILE", "sdd"), "methodology profile on first init: sdd|scrum|kanban")
		httpAddr = flag.String("http", envOr("KANBAN_HTTP", ""), "serve the viz UI + JSON board API on this addr (e.g. :7777); empty disables")
		vizOnly = flag.Bool("viz-only", false, "run only the viz HTTP server (no MCP stdio) for browsing a board")
		sourceKind = flag.String("source", "", "external source to sync into the seeded board, then exit (e.g. docket)")
		docsDir = flag.String("docs-dir", envOr("KANBAN_DOCS_DIR", ""), "for --source=docket: the docket docs dir (e.g. /repo/.docket/docs)")
		repoRoot = flag.String("repo-root", envOr("KANBAN_REPO_ROOT", ""), "base dir for resolving spec/plan paths in card detail (e.g. the docket repo)")
	)
	flag.Parse()

	abs, err := filepath.Abs(*dbPath)
	if err != nil {
		log.Fatalf("resolve db path: %v", err)
	}
	st, err := store.Open(abs)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer st.Close()

	// Default project = the working directory, unless overridden. Boards created
	// without an explicit project land here, so "a project is a directory" by default.
	proj := *project
	if proj == "" {
		if cwd, err := os.Getwd(); err == nil {
			proj = cwd
		}
	}

	ctx := context.Background()
	srv, _ := mcpserver.New(st, *agent, *profile, proj)

	// The viz default board id. When --plan names a board (create-or-select in the
	// default project) we use it as the viz default and as the --source import
	// target; otherwise the viz picks the first board in the db and the SPA's
	// picker switches among all of them.
	focusID := ""
	if *plan != "" {
		p, err := st.CreatePlan(ctx, *plan, proj, "", *profile)
		if err != nil {
			log.Fatalf("select board %q: %v", *plan, err)
		}
		focusID = p.ID
	}

	// One-shot source import: project an external source onto the board named by
	// --plan, then exit. The import needs an explicit target board — require --plan
	// so a shared multi-board db never has its data dumped onto a guessed board.
	if *sourceKind != "" {
		if *plan == "" {
			log.Fatalf("--source requires --plan to name the board to import into")
		}
		provider, err := source.NewProvider(*sourceKind, source.Config{DocsDir: *docsDir})
		if err != nil {
			log.Fatalf("source: %v", err)
		}
		res, err := source.Sync(ctx, st, focusID, provider)
		if err != nil {
			log.Fatalf("%s sync: %v", *sourceKind, err)
		}
		log.Printf("%s sync complete: %d items, %d links onto board %q", *sourceKind, res.Items, res.Links, *plan)
		return
	}

	// Viz layer: the JSON board API + reference SPA. Serve it when --http is set,
	// or always in --viz-only mode. It reads the same store, so it's live. focusID
	// is the default board (may be empty → the viz shows the first board and the
	// SPA picker switches among all boards in the db).
	if *vizOnly {
		addr := *httpAddr
		if addr == "" {
			addr = ":7777"
		}
		log.Printf("kanban-mcp viz-only on http://localhost%s", addr)
		if err := viz.NewServer(st, focusID).WithRepoRoot(*repoRoot).Serve(ctx, addr); err != nil {
			log.Fatalf("viz: %v", err)
		}
		return
	}
	if *httpAddr != "" {
		vzn := viz.NewServer(st, focusID).WithRepoRoot(*repoRoot)
		go func() {
			log.Printf("kanban-mcp viz on http://localhost%s", *httpAddr)
			if err := vzn.Serve(ctx, *httpAddr); err != nil {
				log.Printf("viz server stopped: %v", err)
			}
		}()
	}

	log.Printf("kanban-mcp serving plan %q (db=%s, agent=%s, profile=%s)", *plan, abs, *agent, *profile)
	if err := srv.Run(ctx, &mcp.StdioTransport{}); err != nil {
		log.Fatalf("serve: %v", err)
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
