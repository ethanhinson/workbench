// Command kanban-mcp is an MCP server exposing an SDD-oriented kanban board
// (plan > epic > story > task) over stdio. One --db file holds one shared plan;
// each agent connects with its own --agent identity and swim lane.
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"path/filepath"

	"github.com/ethanhinson/kanban-mcp/internal/docket"
	"github.com/ethanhinson/kanban-mcp/internal/mcpserver"
	"github.com/ethanhinson/kanban-mcp/internal/store"
	"github.com/ethanhinson/kanban-mcp/internal/tui"
	"github.com/ethanhinson/kanban-mcp/internal/viz"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	var (
		dbPath  = flag.String("db", envOr("KANBAN_DB", "kanban.db"), "path to the SQLite plan database")
		plan    = flag.String("plan", envOr("KANBAN_PLAN", "Plan"), "plan name (set on first init)")
		agent   = flag.String("agent", envOr("KANBAN_AGENT", "agent"), "calling agent id (its default swim lane)")
		profile = flag.String("profile", envOr("KANBAN_PROFILE", "sdd"), "methodology profile on first init: sdd|scrum|kanban")
		httpAddr = flag.String("http", envOr("KANBAN_HTTP", ""), "serve the viz UI + JSON board API on this addr (e.g. :7777); empty disables")
		vizOnly = flag.Bool("viz-only", false, "run only the viz HTTP server (no MCP stdio) for browsing a board")
		docketSync = flag.String("docket-sync", "", "import a docket docs dir into the plan, then exit (e.g. /repo/.docket/docs)")
		tuiURL = flag.String("tui", "", "run the terminal UI against an SSE stream URL (e.g. http://localhost:7777/api/stream) and exit")
		repoRoot = flag.String("repo-root", envOr("KANBAN_REPO_ROOT", ""), "base dir for resolving spec/plan paths in card detail (e.g. the docket repo)")
	)
	flag.Parse()

	// TUI is a pure SSE client — it needs no local store or MCP server, so it can
	// point at any served board (local or remote).
	if *tuiURL != "" {
		if err := tui.Run(*tuiURL); err != nil {
			log.Fatalf("tui: %v", err)
		}
		return
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
	srv, mcpSrv, err := mcpserver.New(ctx, st, *plan, *agent, *profile)
	if err != nil {
		log.Fatalf("build server: %v", err)
	}

	// One-shot docket import: sync the backlog into the plan, then exit.
	if *docketSync != "" {
		res, err := docket.NewSyncer(st, mcpSrv.PlanID()).Sync(ctx, *docketSync)
		if err != nil {
			log.Fatalf("docket sync: %v", err)
		}
		log.Printf("docket sync complete: %d changes into %d type lanes", res.Changes, res.Lanes)
		return
	}

	// Viz layer: the JSON board API + reference SPA. Serve it when --http is set,
	// or always in --viz-only mode. It reads the same store, so it's live.
	if *vizOnly {
		addr := *httpAddr
		if addr == "" {
			addr = ":7777"
		}
		log.Printf("kanban-mcp viz-only on http://localhost%s (plan %q)", addr, *plan)
		if err := viz.NewServer(st, mcpSrv.PlanID()).WithRepoRoot(*repoRoot).Serve(ctx, addr); err != nil {
			log.Fatalf("viz: %v", err)
		}
		return
	}
	if *httpAddr != "" {
		vzn := viz.NewServer(st, mcpSrv.PlanID()).WithRepoRoot(*repoRoot)
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
