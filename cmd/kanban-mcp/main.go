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

	"github.com/ethanhinson/kanban-mcp/internal/mcpserver"
	"github.com/ethanhinson/kanban-mcp/internal/store"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	var (
		dbPath = flag.String("db", envOr("KANBAN_DB", "kanban.db"), "path to the SQLite plan database")
		plan   = flag.String("plan", envOr("KANBAN_PLAN", "Plan"), "plan name (set on first init)")
		agent  = flag.String("agent", envOr("KANBAN_AGENT", "agent"), "calling agent id (its default swim lane)")
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

	ctx := context.Background()
	srv, _, err := mcpserver.New(ctx, st, *plan, *agent)
	if err != nil {
		log.Fatalf("build server: %v", err)
	}

	log.Printf("kanban-mcp serving plan %q (db=%s, agent=%s)", *plan, abs, *agent)
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
