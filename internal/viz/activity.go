package viz

import (
	"context"
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
	Harness   string `json:"harness"`    // "claude-code" | "codex" | "cursor" | "grok" | "fuse"
	EventType string `json:"event_type"` // the 7 LCD kinds; see normalizeKind
	SessionID string `json:"session_id"` // the normalized per-conversation key (see adapters)
	Project   string `json:"project,omitempty"` // repo/cwd the session works in; seeds session→board routing
	Tool      string `json:"tool,omitempty"`
	Target    string `json:"target,omitempty"` // a short human label (file, command head, subagent type)
	Status    string `json:"status,omitempty"` // "ok" | "error" | ""
	AgentID   string `json:"agent_id,omitempty"`
	AgentType string `json:"agent_type,omitempty"`
}

// nativeHook is the union of the hook-payload shapes across supported harnesses.
// Research (2026-08) established that every harness puts a per-conversation session
// id and a project/cwd path in its hook payload, but under DIFFERENT field names and
// casing — so this one struct captures every variant (the keys don't collide) and
// the adapters below pick whichever the harness populated:
//
//	harness       session field       project field           event-name field
//	Claude Code   session_id          cwd                     hook_event_name
//	Codex         session_id          cwd                     hook_event_name
//	Cursor        conversation_id     workspace_roots[0]      hook_event_name
//	Grok          sessionId           workspaceRoot / cwd     hookEventName
//
// Adding a harness = teach these tags its field name, no new struct. HTTP-hook
// harnesses (Claude, Grok) POST this directly; stdio-only ones (Cursor, Codex) POST
// it from a curl command hook.
type nativeHook struct {
	// event-name variants
	HookEventName  string `json:"hook_event_name"`
	HookEventNameC string `json:"hookEventName"`
	// session-id variants
	SessionID      string `json:"session_id"`
	SessionIDC     string `json:"sessionId"`
	ConversationID string `json:"conversation_id"`
	// project/cwd variants
	CWD            string   `json:"cwd"`
	WorkspaceRoot  string   `json:"workspaceRoot"`
	WorkspaceRoots []string `json:"workspace_roots"`
	// tool + subagent context (names are consistent enough to share)
	ToolName     string          `json:"tool_name"`
	ToolNameC    string          `json:"toolName"`
	ToolInput    json.RawMessage `json:"tool_input"`
	ToolInputC   json.RawMessage `json:"toolInput"`
	ToolResponse json.RawMessage `json:"tool_response"`
	AgentID      string          `json:"agent_id"`
	AgentType    string          `json:"agent_type"`
}

// firstNonEmpty returns the first non-empty string, so an adapter can collapse a
// harness's field-name variants to one value.
func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if v != "" {
			return v
		}
	}
	return ""
}

// eventName / sessionID / project / toolName / toolInput collapse the native
// variants a harness may have used into the single value the LCD contract wants.
func (h nativeHook) eventName() string { return firstNonEmpty(h.HookEventName, h.HookEventNameC) }
func (h nativeHook) sessionID() string {
	return firstNonEmpty(h.SessionID, h.SessionIDC, h.ConversationID)
}
func (h nativeHook) project() string {
	if len(h.WorkspaceRoots) > 0 {
		return h.WorkspaceRoots[0]
	}
	return firstNonEmpty(h.WorkspaceRoot, h.CWD)
}
func (h nativeHook) toolName() string             { return firstNonEmpty(h.ToolName, h.ToolNameC) }
func (h nativeHook) toolInput() json.RawMessage {
	if len(h.ToolInput) > 0 {
		return h.ToolInput
	}
	return h.ToolInputC
}

// harnessFor guesses the harness label from which field variants were populated —
// purely cosmetic (the card's `harness` chip); routing never depends on it.
func (h nativeHook) harnessFor() string {
	switch {
	case h.ConversationID != "":
		return "cursor"
	case h.SessionIDC != "" || h.HookEventNameC != "":
		return "grok"
	default:
		return "claude-code" // Claude and Codex share snake_case; both card fine as-is
	}
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

// parseActivity accepts either a native harness hook payload (any supported
// harness — see nativeHook) or a pre-normalized ActivityEvent, and returns the
// normalized event. ok=false means "nothing worth projecting" (unparseable, or a
// lifecycle event we don't card).
func parseActivity(body []byte) (ActivityEvent, bool) {
	// Prefer the native hook shape when an event-name field (any variant) is present.
	var h nativeHook
	if json.Unmarshal(body, &h) == nil && h.eventName() != "" {
		return fromNativeHook(h)
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

// normalizeKind maps a harness's native hook-event name to one of the LCD event
// kinds. Event names are consistent enough across harnesses (all four use the same
// PascalCase names) to share one map; the "" return means "a hook we don't card".
func normalizeKind(name string) (kind, status string, ok bool) {
	switch name {
	case "PostToolUse", "afterFileEdit", "afterShellExecution", "afterMCPExecution":
		return "tool_use_complete", "ok", true
	case "PostToolUseFailure":
		return "tool_use_failed", "error", true
	case "PreToolUse", "beforeShellExecution", "beforeMCPExecution":
		return "tool_use_start", "", true
	case "SubagentStart", "subagentStart":
		return "subagent_start", "", true
	case "SubagentStop", "subagentStop":
		return "subagent_stop", "", true
	case "SessionStart", "sessionStart":
		return "session_start", "", true
	case "SessionEnd", "sessionEnd", "Stop", "stop":
		return "session_end", "", true
	default:
		return "", "", false // compaction, config, prompt-submit, etc. — not carded
	}
}

// fromNativeHook maps any supported harness's native hook payload to the LCD
// contract, collapsing the harness's field-name variants (see nativeHook).
func fromNativeHook(h nativeHook) (ActivityEvent, bool) {
	kind, status, ok := normalizeKind(h.eventName())
	if !ok {
		return ActivityEvent{}, false
	}
	ev := ActivityEvent{
		Harness:   h.harnessFor(),
		EventType: kind,
		SessionID: h.sessionID(),
		Project:   h.project(),
		Tool:      h.toolName(),
		Status:    status,
		AgentID:   h.AgentID,
		AgentType: h.AgentType,
	}
	switch {
	case strings.HasPrefix(kind, "tool_use"):
		ev.Target = toolTarget(h.toolName(), h.toolInput())
	case strings.HasPrefix(kind, "subagent"):
		ev.Target = h.AgentType
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

// activityBoard resolves which board an activity event targets — scoped to "the
// board the harness session is working on", never a global default. Resolution
// order, each step falling through to the next:
//
//  1. session_id → board   the LEARNED mapping (fast path; O(1) once seeded)
//  2. project path → board  seed the map: resolve the event's project (from the
//     payload, else ?project=) to that project's board, then RECORD session→board
//     so every later event for this session takes step 1
//  3. ?board=<id>           an explicit board id on the URL (validated)
//  4. ""                    unresolved — caller drops the event (never pollutes a
//     default board)
//
// This is the harness-agnostic routing: research established no harness reliably
// hands a spawned MCP server its session id, so the map can't be written from the
// MCP side — it is LEARNED here from the activity stream, which universally carries
// both the session id and a project/cwd path in the hook payload.
func (s *Server) activityBoard(r *http.Request, ev ActivityEvent) string {
	ctx := r.Context()

	// 1. Learned session → board.
	if id, ok := s.st.BoardForSession(ctx, ev.SessionID); ok {
		return id
	}

	// 2. Seed from project: payload project first, then a ?project= override.
	proj := firstNonEmpty(ev.Project, r.URL.Query().Get("project"))
	if proj != "" {
		if id := s.boardForProject(ctx, proj); id != "" {
			if ev.SessionID != "" {
				// Learn it so subsequent events for this session hit the fast path and
				// stay stable even if a later event lacks the project field.
				_ = s.st.RecordSessionBoard(ctx, ev.SessionID, id)
			}
			return id
		}
	}

	// 3. Explicit board id on the URL (a deliberate override / testing hook).
	if id := r.URL.Query().Get("board"); id != "" {
		if _, err := s.st.LoadPlan(ctx, id); err == nil {
			if ev.SessionID != "" {
				_ = s.st.RecordSessionBoard(ctx, ev.SessionID, id)
			}
			return id
		}
	}

	// 4. Unresolved — drop.
	return ""
}

// boardForProject returns the board a project's activity should land on. When a
// project has several boards (the multiple-boards-per-project case), the most
// recently created wins — the session is most likely working on the newest board.
func (s *Server) boardForProject(ctx context.Context, project string) string {
	boards, err := s.st.ListPlansForProject(ctx, project)
	if err != nil || len(boards) == 0 {
		return ""
	}
	// ListPlansForProject orders by created_at ascending, so the last is newest.
	return boards[len(boards)-1].ID
}

// projectActivity upserts one activity event as a card on the board's Activity
// view. Cards are keyed by a per-event ext_key so the same delivery never
// duplicates. The card lands in view:activity, laned by session, tagged by the
// event kind — a live feed of "what the agent is doing".
func (s *Server) projectActivity(r *http.Request, ev ActivityEvent) error {
	planID := s.activityBoard(r, ev)
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
