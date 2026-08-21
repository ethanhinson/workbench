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

	"github.com/ethanhinson/kanban-mcp/internal/store"
)

//go:embed static/*
var staticFS embed.FS

// Server serves the board API + reference SPA for a single plan.
type Server struct {
	st     *store.Store
	planID string
}

func NewServer(st *store.Store, planID string) *Server {
	return &Server{st: st, planID: planID}
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
	mux.Handle("/", http.FileServer(http.FS(sub)))

	mux.HandleFunc("/api/board", s.handleBoard)
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
