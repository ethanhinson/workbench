package viz

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync/atomic"

	"github.com/ethanhinson/workbench/internal/board"
)

// activitySeq is a process-local monotonic counter giving each activity event a
// unique ext_key suffix. The broker revision is NOT unique per event (it only
// bumps on store mutations, and resets on restart), so keying on it collided and
// upserts overwrote sibling events. An atomic counter never collides within a
// server's lifetime; across restarts the session prefix keeps old cards distinct.
var activitySeq atomic.Uint64

// activityMax bounds a request body so a runaway hook can't feed us an unbounded
// payload. Hook events are small; 256KiB is generous.
const activityMax = 256 << 10

// ActivityEvent is the harness-agnostic activity contract — the lowest common
// denominator every agent harness (Claude Code, Codex, Cursor, fuse) can emit.
// The /api/activity endpoint accepts either this normalized shape directly, or a
// harness's native hook payload (currently Claude Code's), which it normalizes
// into this before projecting. Unknown fields are ignored, never rejected: a
// hook is fire-and-forget and must never stall the agent on a schema quibble.
type ActivityEvent struct {
	Harness   string `json:"harness"`    // "claude-code" | "codex" | "cursor" | "fuse"
	EventType string `json:"event_type"` // the 7 LCD kinds; see normalizeKind
	SessionID string `json:"session_id"`
	Tool      string `json:"tool,omitempty"`
	Target    string `json:"target,omitempty"` // a short human label (file, command head, subagent type)
	Status    string `json:"status,omitempty"` // "ok" | "error" | ""
	AgentID   string `json:"agent_id,omitempty"`
	AgentType string `json:"agent_type,omitempty"`
}

// claudeHook is the subset of Claude Code's native hook JSON we read. Claude's
// `type:"http"` hook POSTs the full event here with no shell script, so the
// Claude adapter is pure settings.json config — this struct is that adapter.
type claudeHook struct {
	HookEventName string          `json:"hook_event_name"`
	SessionID     string          `json:"session_id"`
	ToolName      string          `json:"tool_name"`
	ToolInput     json.RawMessage `json:"tool_input"`
	ToolResponse  json.RawMessage `json:"tool_response"`
	AgentID       string          `json:"agent_id"`
	AgentType     string          `json:"agent_type"`
}

// handleActivity ingests one harness activity event and projects it onto the
// board's Activity view as a card. It ALWAYS answers 2xx quickly (even on a
// malformed body) so a hook never blocks or errors the agent that fired it —
// observability must be lossy-safe, never load-bearing. The projection is keyed
// by ext_key so re-delivery is idempotent (no duplicate cards).
func (s *Server) handleActivity(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	body, _ := io.ReadAll(io.LimitReader(r.Body, activityMax))

	ev, ok := parseActivity(body)
	if !ok {
		// Not fatal: ack so the hook moves on. Nothing to project.
		w.WriteHeader(http.StatusNoContent)
		return
	}

	if err := s.projectActivity(r, ev); err != nil {
		// Log for us, but still ack the hook — the agent must not stall.
		log.Printf("viz: activity project failed: %v", err)
	}
	w.WriteHeader(http.StatusNoContent)
}

// parseActivity accepts either a normalized ActivityEvent or a Claude Code hook
// payload and returns the normalized event. ok=false means "nothing worth
// projecting" (unparseable, or a lifecycle event we don't card).
func parseActivity(body []byte) (ActivityEvent, bool) {
	// Prefer the native hook shape when hook_event_name is present.
	var ch claudeHook
	if json.Unmarshal(body, &ch) == nil && ch.HookEventName != "" {
		return fromClaudeHook(ch)
	}
	var ev ActivityEvent
	if json.Unmarshal(body, &ev) == nil && ev.EventType != "" {
		if ev.Harness == "" {
			ev.Harness = "unknown"
		}
		return ev, true
	}
	return ActivityEvent{}, false
}

// fromClaudeHook maps Claude Code's hook payload to the LCD contract.
func fromClaudeHook(ch claudeHook) (ActivityEvent, bool) {
	ev := ActivityEvent{
		Harness:   "claude-code",
		SessionID: ch.SessionID,
		Tool:      ch.ToolName,
		AgentID:   ch.AgentID,
		AgentType: ch.AgentType,
	}
	switch ch.HookEventName {
	case "PostToolUse":
		ev.EventType = "tool_use_complete"
		ev.Target = toolTarget(ch.ToolName, ch.ToolInput)
		ev.Status = "ok"
	case "PostToolUseFailure":
		ev.EventType = "tool_use_failed"
		ev.Target = toolTarget(ch.ToolName, ch.ToolInput)
		ev.Status = "error"
	case "PreToolUse":
		ev.EventType = "tool_use_start"
		ev.Target = toolTarget(ch.ToolName, ch.ToolInput)
	case "SubagentStart":
		ev.EventType = "subagent_start"
		ev.Target = ch.AgentType
	case "SubagentStop":
		ev.EventType = "subagent_stop"
		ev.Target = ch.AgentType
	case "SessionStart":
		ev.EventType = "session_start"
	case "SessionEnd", "Stop":
		ev.EventType = "session_end"
	default:
		return ev, false // a hook we don't card (compaction, config, etc.)
	}
	return ev, true
}

// toolTarget extracts a short human label from a tool's input — the file for
// Read/Edit/Write, the command head for Bash — so an activity card reads like
// "Bash: go test ./..." rather than an opaque tool name. Best-effort; a shape it
// doesn't recognize yields "".
func toolTarget(tool string, input json.RawMessage) string {
	if len(input) == 0 {
		return ""
	}
	var m map[string]any
	if json.Unmarshal(input, &m) != nil {
		return ""
	}
	pick := func(keys ...string) string {
		for _, k := range keys {
			if v, ok := m[k].(string); ok && v != "" {
				return v
			}
		}
		return ""
	}
	switch tool {
	case "Bash":
		return firstLine(cleanCommand(pick("command")), 60)
	case "Read", "Edit", "Write", "NotebookEdit":
		return shortPath(pick("file_path", "notebook_path"))
	case "Grep":
		return pick("pattern")
	case "Glob":
		return pick("pattern")
	case "Task", "Agent":
		return pick("description", "subagent_type")
	}
	// MCP tools and anything else: no reliable target field.
	return ""
}

// cleanCommand strips the harness's shell wrapper from a Bash command so the
// activity card shows the actual command the agent ran, not the boilerplate the
// runtime prepends. Harnesses wrap commands as `cd <dir> && <real command>` (and
// sometimes source a shell snapshot first); without stripping, every card reads
// as an identical `cd /path/to/repo`. It removes leading `cd <dir> &&` segments
// (repeatedly, in case of nesting) and returns the meaningful tail. A command
// that is ONLY a `cd` is left intact so it still reads as something.
func cleanCommand(s string) string {
	s = strings.TrimSpace(s)
	for {
		rest, ok := stripCdPrefix(s)
		if !ok || rest == "" {
			break
		}
		s = strings.TrimSpace(rest)
	}
	return s
}

// stripCdPrefix removes a single leading `cd <token> &&` (or `cd <token> ;`) and
// returns the remainder. ok=false when s does not start with such a segment.
func stripCdPrefix(s string) (string, bool) {
	if !strings.HasPrefix(s, "cd ") {
		return s, false
	}
	// Find the connector that ends the cd segment: "&&" or ";".
	amp := strings.Index(s, "&&")
	semi := strings.Index(s, ";")
	cut := -1
	switch {
	case amp >= 0 && (semi < 0 || amp < semi):
		cut = amp + 2
	case semi >= 0:
		cut = semi + 1
	}
	if cut < 0 {
		return s, false // a bare `cd <dir>` with no following command; keep it
	}
	return s[cut:], true
}

func firstLine(s string, max int) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > max {
		s = s[:max] + "…"
	}
	return s
}

func shortPath(p string) string {
	if p == "" {
		return ""
	}
	parts := strings.Split(strings.TrimRight(p, "/"), "/")
	if n := len(parts); n >= 2 {
		return parts[n-2] + "/" + parts[n-1]
	}
	return parts[len(parts)-1]
}

// activityBoard resolves which board an activity event targets, WITHOUT the
// default-board fallback that browser reads use. Resolution order:
//
//	?board=<id>       an explicit board id (validated), else
//	?project=<path>   the board for that project (its sole/first board), else
//	""                unresolved — caller drops the event
//
// This is what makes activity "scoped to the board the session is working on":
// the per-project harness hook carries its project, and events land only on that
// project's board — never on an unrelated default.
func (s *Server) activityBoard(r *http.Request) string {
	if id := r.URL.Query().Get("board"); id != "" {
		if _, err := s.st.LoadPlan(r.Context(), id); err == nil {
			return id
		}
	}
	if proj := r.URL.Query().Get("project"); proj != "" {
		if boards, err := s.st.ListPlansForProject(r.Context(), proj); err == nil && len(boards) > 0 {
			return boards[0].ID
		}
	}
	return ""
}

// projectActivity upserts one activity event as a card on the board's Activity
// view. Cards are keyed by a per-event ext_key so the same delivery never
// duplicates. The card lands in view:activity, laned by session, tagged by the
// event kind — a live feed of "what the agent is doing".
func (s *Server) projectActivity(r *http.Request, ev ActivityEvent) error {
	planID := s.activityBoard(r)
	if planID == "" {
		// No board explicitly targeted. Unlike a browser GET (which falls back to a
		// default board so the UI always renders something), activity must NOT
		// piggyback onto whatever board happens to be default — that pollutes an
		// unrelated work board with a session's tool-call log. Drop it instead; the
		// hook is fire-and-forget, so a dropped event is the correct lossy-safe
		// outcome when the harness didn't say which board it's working on.
		return nil
	}
	// A monotonic, collision-free per-event key: session prefix + a process-local
	// atomic counter. Each event gets its own card (the store stamps created_at for
	// ordering); no two events ever share a key within this server's lifetime.
	sess := shortID(ev.SessionID)
	extKey := fmt.Sprintf("activity:%s:%d", sess, activitySeq.Add(1))

	title := ev.EventType
	if ev.Tool != "" {
		title = ev.Tool
	}
	if ev.Target != "" {
		title = title + " — " + ev.Target
	}

	// The Activity view is a lanes view keyed by event category (tool/subagent/
	// session), and the SPA buckets a card by its lane: label. So lane: carries the
	// category and group: carries the session id (rendered as a per-card chip).
	labels := []board.Label{
		{NS: "view", Value: "activity"},
		{NS: "lane", Value: groupForEvent(ev)},
		{NS: "group", Value: laneForSession(sess)},
	}

	it := &board.Item{
		PlanID:    planID,
		Kind:      board.KindTask,
		Title:     title,
		Body:      activityBody(ev),
		ColumnKey: "backlog", // activity lives in its own view, not the workflow columns
		ExtKey:    extKey,
		Labels:    labels,
	}
	_, err := s.st.UpsertByExtKey(r.Context(), "activity", it)
	return err
}

func activityBody(ev ActivityEvent) string {
	var b strings.Builder
	fmt.Fprintf(&b, "**%s** · `%s`", ev.EventType, ev.Harness)
	if ev.Status != "" {
		fmt.Fprintf(&b, " · %s", ev.Status)
	}
	if ev.AgentType != "" {
		fmt.Fprintf(&b, "\n\nsubagent: %s", ev.AgentType)
	}
	return b.String()
}

// groupForEvent buckets an event for the Activity view's lanes: tool activity,
// subagent lifecycle, or session lifecycle.
func groupForEvent(ev ActivityEvent) string {
	switch {
	case strings.HasPrefix(ev.EventType, "tool_use"):
		return "tool"
	case strings.HasPrefix(ev.EventType, "subagent"):
		return "subagent"
	default:
		return "session"
	}
}

func laneForSession(sess string) string {
	if sess == "" {
		return "session"
	}
	return sess
}

func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}
