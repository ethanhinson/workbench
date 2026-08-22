// Command demo drives a live SDD workflow against a real kanban-mcp MCP server
// while serving the browser board, so each MCP tool call pushes to the UI over
// SSE. It steps through the lifecycle of one fuse change (#74) with pauses so the
// board can be observed/screenshotted between transitions.
//
// Not part of the shipped product — a scripted demonstration harness.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/ethanhinson/kanban-mcp/internal/mcpserver"
	"github.com/ethanhinson/kanban-mcp/internal/source"
	"github.com/ethanhinson/kanban-mcp/internal/store"
	"github.com/ethanhinson/kanban-mcp/internal/viz"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	db := flag.String("db", "/tmp/fuse-demo.db", "board db")
	addr := flag.String("http", ":7800", "viz http addr")
	docsDir := flag.String("docket", os.ExpandEnv("$HOME/dev/fuse/.docket/docs"), "docket docs dir")
	step := flag.String("step", "", "run a single named step and exit (import|spec|specd|start|block|unblock|review|done)")
	pause := flag.Duration("pause", 4*time.Second, "pause between steps in full-run mode")
	flag.Parse()
	ctx := context.Background()

	st, err := store.Open(*db)
	if err != nil {
		log.Fatal(err)
	}
	defer st.Close()

	srv, mcpSrv, err := mcpserver.New(ctx, st, "Fuse Backlog", "agent", "docket")
	if err != nil {
		log.Fatal(err)
	}
	planID := mcpSrv.PlanID()

	// Connect an in-memory MCP client — this is exactly how an agent drives it.
	s2c, c2s := mcp.NewInMemoryTransports()
	if _, err := srv.Connect(ctx, s2c, nil); err != nil {
		log.Fatal(err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "demo", Version: "1"}, nil)
	cs, err := client.Connect(ctx, c2s, nil)
	if err != nil {
		log.Fatal(err)
	}

	// Serve the board so the browser can watch live over SSE.
	go func() { _ = viz.NewServer(st, planID).Serve(ctx, *addr) }()
	log.Printf("board live at http://localhost%s", *addr)

	// Resolve #74's card id (created by the import step).
	itemID := func() string {
		id, _ := st.ItemIDByExtKey(ctx, planID, "docket:74")
		return id
	}

	call := func(name string, args map[string]any) {
		res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil {
			log.Fatalf("%s: %v", name, err)
		}
		txt := ""
		for _, c := range res.Content {
			if tc, ok := c.(*mcp.TextContent); ok {
				txt += tc.Text
			}
		}
		status := "ok"
		if res.IsError {
			status = "ERROR"
		}
		log.Printf("→ %-16s [%s] %s", name, status, oneline(txt))
	}

	steps := map[string]func(){
		"import": func() {
			provider, err := source.NewProvider("docket", source.Config{DocsDir: *docsDir})
			if err != nil {
				log.Fatal(err)
			}
			r, err := source.Sync(ctx, st, planID, provider)
			if err != nil {
				log.Fatal(err)
			}
			log.Printf("→ import           [ok] %d items, %d links", r.Items, r.Links)
		},
		"spec": func() {
			call("item_set_spec", map[string]any{"item_id": itemID(), "spec_ref": "docs/specs/74-sandbox-health.md", "status": "draft"})
			call("item_move", map[string]any{"item_id": itemID(), "column": "specifying"})
			call("item_comment", map[string]any{"item_id": itemID(), "text": "Drafting the KindSandboxHealth emitter design."})
		},
		"specd": func() {
			call("item_set_spec", map[string]any{"item_id": itemID(), "spec_ref": "docs/specs/74-sandbox-health.md", "status": "approved"})
			call("item_move", map[string]any{"item_id": itemID(), "column": "specd"})
		},
		"start": func() {
			call("item_label", map[string]any{"item_id": itemID(), "labels": []string{"priority:p1"}})
			call("item_move", map[string]any{"item_id": itemID(), "column": "in_progress"})
		},
		"block": func() {
			call("item_set_blocked", map[string]any{"item_id": itemID(), "blocked": true, "reason": "waiting on #63 container substrate to land emitter hook"})
		},
		"unblock": func() {
			call("item_set_blocked", map[string]any{"item_id": itemID(), "blocked": false})
		},
		"review": func() {
			call("item_move", map[string]any{"item_id": itemID(), "column": "review"})
			call("item_comment", map[string]any{"item_id": itemID(), "text": "PR opened; E2E asserts the metric now gains a series."})
		},
		"done": func() {
			call("item_move", map[string]any{"item_id": itemID(), "column": "done"})
		},
	}

	order := []string{"import", "spec", "specd", "start", "block", "unblock", "review", "done"}

	if *step != "" {
		fn, ok := steps[*step]
		if !ok {
			log.Fatalf("unknown step %q", *step)
		}
		fn()
		time.Sleep(300 * time.Millisecond) // let SSE flush
		return
	}

	// Full run with pauses (interactive observation).
	for _, name := range order {
		steps[name]()
		time.Sleep(*pause)
	}
	log.Print("demo complete; board still live")
	select {}
}

func oneline(s string) string {
	var m map[string]any
	if json.Unmarshal([]byte(s), &m) == nil {
		if msg, ok := m["message"].(string); ok {
			return msg
		}
	}
	if len(s) > 80 {
		return s[:80]
	}
	return s
}

var _ = fmt.Sprintf
