// Package tui is a terminal renderer for the kanban board. It is a thin client
// over the same SSE stream the browser uses (GET /api/stream), so a TUI can run
// locally or against a remote served board with no extra server code.
package tui

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/ethanhinson/kanban-mcp/internal/board"
)

// streamSnapshots connects to an SSE endpoint and calls onSnap for every "board"
// event. It blocks until ctx is cancelled or the connection fails; the caller is
// responsible for reconnect policy.
func streamSnapshots(ctx context.Context, url string, onSnap func(board.Snapshot)) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "text/event-stream")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	var event, data string
	for sc.Scan() {
		line := sc.Text()
		switch {
		case line == "": // end of one SSE frame — dispatch it
			if event == "board" && data != "" {
				var snap board.Snapshot
				if err := json.Unmarshal([]byte(data), &snap); err == nil {
					onSnap(snap)
				}
			}
			event, data = "", ""
		case strings.HasPrefix(line, "event: "):
			event = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			data += strings.TrimPrefix(line, "data: ")
		}
	}
	return sc.Err()
}
