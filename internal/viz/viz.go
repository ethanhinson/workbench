// Package viz is the pluggable visualization layer. It exposes the board's
// renderer-agnostic Snapshot contract over a small JSON HTTP API and ships a
// zero-build reference SPA. The API is the seam: any renderer (the bundled SPA,
// an agent-generated component, a TUI, a static export) consumes /api/board.
package viz

import (
	"context"
	"embed"
	"encoding/json"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/ethanhinson/kanban-mcp/internal/store"
)

//go:embed static/*
var staticFS embed.FS

// Server serves the board API + reference SPA for a single plan.
type Server struct {
	st       *store.Store
	planID   string
	repoRoot string // base dir for resolving spec/plan ref paths (docket repo); may be ""
}

func NewServer(st *store.Store, planID string) *Server {
	return &Server{st: st, planID: planID}
}

// WithRepoRoot sets the base directory used to resolve an item's spec_ref/plan
// path to file content in the detail endpoint.
func (s *Server) WithRepoRoot(root string) *Server {
	s.repoRoot = root
	return s
}

// Handler returns the HTTP handler. Routes:
//
//	GET /                 the reference SPA (single page)
//	GET /api/board        the Snapshot JSON contract
//	GET /api/profiles     the built-in methodology presets (for renderers)
//	GET /healthz          liveness
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	sub, _ := fs.Sub(staticFS, "static")
	// no-store so a rebuilt SPA is never served stale from browser cache.
	fileSrv := http.FileServer(http.FS(sub))
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store, must-revalidate")
		fileSrv.ServeHTTP(w, r)
	}))

	mux.HandleFunc("/api/board", s.handleBoard)
	mux.HandleFunc("/api/stream", s.handleStream)
	mux.HandleFunc("/api/item/", s.handleItem)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})
	return withCORS(mux)
}

func (s *Server) handleBoard(w http.ResponseWriter, r *http.Request) {
	snap, err := s.st.Snapshot(r.Context(), s.planID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, snap)
}

// handleItem returns the full detail for one card, including rendered spec/plan
// content (resolved under repoRoot) and bidirectional dependency refs.
func (s *Server) handleItem(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/item/")
	if id == "" {
		http.Error(w, "missing item id", http.StatusBadRequest)
		return
	}
	detail, err := s.st.ItemDetail(r.Context(), s.planID, id, s.readRef)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	writeJSON(w, detail)
}

// readRef resolves a stored spec/plan ref path to file content under repoRoot.
// It refuses paths escaping the root and caps size. Returns "" on any failure.
func (s *Server) readRef(ref string) string {
	if s.repoRoot == "" || ref == "" {
		return ""
	}
	clean := filepath.Clean("/" + ref) // strip leading .. traversal
	full := filepath.Join(s.repoRoot, clean)
	if !strings.HasPrefix(full, filepath.Clean(s.repoRoot)) {
		return ""
	}
	b, err := os.ReadFile(full)
	if err != nil {
		return ""
	}
	if len(b) > 256*1024 {
		b = b[:256*1024]
	}
	return string(b)
}

// handleStream is a Server-Sent Events endpoint that pushes a fresh board
// Snapshot on every store mutation (and once on connect). Both the browser SPA
// (EventSource) and the TUI (an HTTP SSE reader) consume this single stream, so
// there is one push path with two renderers. Read-only: no client->server frames,
// which is why SSE fits better than WebSockets here.
func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	changes, unsub := s.st.Broker().Subscribe()
	defer unsub()

	send := func() bool {
		snap, err := s.st.Snapshot(r.Context(), s.planID)
		if err != nil {
			return false
		}
		b, err := json.Marshal(snap)
		if err != nil {
			return false
		}
		// SSE frame: one "data:" line (compact JSON, no embedded newlines) + blank line.
		if _, err := w.Write([]byte("event: board\ndata: ")); err != nil {
			return false
		}
		if _, err := w.Write(b); err != nil {
			return false
		}
		if _, err := w.Write([]byte("\n\n")); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}

	if !send() { // initial frame so a new client renders immediately
		return
	}
	for {
		select {
		case <-r.Context().Done():
			return
		case _, ok := <-changes:
			if !ok || !send() {
				return
			}
		}
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

// withCORS allows any origin so an on-demand / externally-hosted renderer can
// fetch the board API directly.
func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Serve runs the viz server until ctx is cancelled.
func (s *Server) Serve(ctx context.Context, addr string) error {
	srv := &http.Server{Addr: addr, Handler: s.Handler()}
	go func() {
		<-ctx.Done()
		srv.Close()
	}()
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}
