// Command workbench is an MCP server exposing SDD-oriented kanban boards over
// stdio. One --db file hosts many boards, grouped by project (a directory path);
// the agent creates or selects a board at runtime with board_start, and each agent
// connects with its own --agent identity and swim lane.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/ethanhinson/workbench/internal/mcpserver"
	"github.com/ethanhinson/workbench/internal/store"
	"github.com/ethanhinson/workbench/internal/viz"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// version is the release version, stamped at build time by the release workflow
// via -ldflags "-X main.version=<tag>". A plain `go build` leaves it as "dev" so
// `workbench --version` always prints something meaningful.
var version = "dev"

// writeVersion prints the stamped version. Kept separate from main so the
// release binaries' self-identification is unit-testable without booting the
// server.
func writeVersion(w io.Writer) {
	fmt.Fprintf(w, "workbench %s\n", version)
}

func main() {
	var (
		dbPath   = flag.String("db", envOr("KANBAN_DB", "kanban.db"), "path to the SQLite plan database")
		plan     = flag.String("plan", envOr("KANBAN_PLAN", ""), "board to focus as the viz default (create-or-select by name). Empty => agents create boards at runtime via board_start")
		project  = flag.String("project", envOr("KANBAN_PROJECT", ""), "default project (a directory path) new boards belong to; empty => the working directory")
		agent    = flag.String("agent", envOr("KANBAN_AGENT", "agent"), "calling agent id (its default swim lane)")
		profile  = flag.String("profile", envOr("KANBAN_PROFILE", "sdd"), "methodology profile on first init: sdd|scrum|kanban")
		httpAddr = flag.String("http", envOr("KANBAN_HTTP", ""), "serve the viz UI + JSON board API on this addr (e.g. :7777); empty disables")
		vizOnly  = flag.Bool("viz-only", false, "run only the viz HTTP server (no MCP stdio) for browsing a board")
		showVer  = flag.Bool("version", false, "print the workbench version and exit")
	)
	flag.Parse()

	if *showVer {
		writeVersion(os.Stdout)
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

	// Viz layer: the JSON board API + reference SPA. Serve it when --http is set,
	// or always in --viz-only mode. It reads the same store, so it's live. focusID
	// is the default board (may be empty → the viz shows the first board and the
	// SPA picker switches among all boards in the db).
	if *vizOnly {
		addr := *httpAddr
		if addr == "" {
			addr = ":7777"
		}
		// Default a host-less addr to loopback so a bare --http :7777 does not
		// silently expose the board on every interface. Normalize once, here, so the
		// logged URL and the actual bind can never disagree.
		addr = viz.NormalizeAddr(addr)
		// Watch bridges writes made by other processes (the MCP server) into this
		// reader's broker, so the browser goes live on foreign writes. Without it a
		// viz-only server is a static reader that only reflects writes on refresh.
		go st.Watch(ctx, 0)
		log.Printf("workbench viz-only on %s", browseURL(addr))
		if err := viz.NewServer(st, focusID).Serve(ctx, addr); err != nil {
			log.Fatalf("viz: %v", err) // a failed bind (e.g. port in use) is fatal, not silent
		}
		return
	}
	if *httpAddr != "" {
		addr := viz.NormalizeAddr(*httpAddr) // loopback default; see the viz-only branch
		vzn := viz.NewServer(st, focusID)
		// Also watch here: even the combined MCP+viz server may share its db file
		// with other agents' processes, whose writes must reach this browser too.
		go st.Watch(ctx, 0)
		go func() {
			log.Printf("workbench viz on %s", browseURL(addr))
			// A bind failure here (e.g. the port is already taken by a stray viz
			// process) previously died silently in this goroutine, leaving the MCP
			// server up but the browser dark. Make it loud and fatal instead.
			if err := vzn.Serve(ctx, addr); err != nil {
				log.Fatalf("viz server failed on %s: %v", addr, err)
			}
		}()
	}

	log.Printf("workbench serving plan %q (db=%s, agent=%s, profile=%s)", *plan, abs, *agent, *profile)
	if err := srv.Run(ctx, &mcp.StdioTransport{}); err != nil {
		log.Fatalf("serve: %v", err)
	}
}

// browseURL renders a human-clickable URL for a listen address in any of the
// forms flag accepts: ":7777" (port only), "127.0.0.1:7777", "0.0.0.0:7777".
// A bare or wildcard host is shown as localhost so the log line is clickable.
func browseURL(addr string) string {
	host, port := addr, ""
	if i := strings.LastIndex(addr, ":"); i >= 0 {
		host, port = addr[:i], addr[i+1:]
	}
	if host == "" || host == "0.0.0.0" || host == "::" || host == "[::]" {
		host = "localhost"
	}
	if port == "" {
		return "http://" + host
	}
	return "http://" + host + ":" + port
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
