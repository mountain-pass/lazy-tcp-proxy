package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/mountain-pass/lazy-tcp-proxy/internal/config"
)

// reloadFunc re-discovers targets and applies the config overlay to the proxy.
type reloadFunc func(ctx context.Context) error

// Server is the authenticated admin HTTP server.
type Server struct {
	store  *config.Store
	reload reloadFunc
	apiKey string
}

// New creates an admin Server.
func New(store *config.Store, reload reloadFunc, apiKey string) *Server {
	return &Server{store: store, reload: reload, apiKey: apiKey}
}

// Run starts the admin HTTP server on the given port. It shuts down gracefully
// when ctx is cancelled.
func (s *Server) Run(ctx context.Context, port int) {
	mux := http.NewServeMux()
	mux.Handle("GET /config", s.auth(s.handleGetConfig))
	mux.Handle("GET /config/reload", s.auth(s.handleReload))
	mux.Handle("PUT /config/update", s.auth(s.handleUpdate))

	hs := &http.Server{Addr: fmt.Sprintf(":%d", port), Handler: mux}
	context.AfterFunc(ctx, func() {
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		hs.Shutdown(shutCtx) //nolint:errcheck
	})
	log.Printf("admin server: listening on :%d", port)
	if err := hs.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("admin server: %v", err)
	}
}

// auth is middleware that validates the X-API-Key header.
func (s *Server) auth(next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-Key") != s.apiKey {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		next(w, r)
	})
}

// handleGetConfig returns the current in-memory config as JSON.
func (s *Server) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.store.Get())
}

// handleReload re-reads the config file from disk and re-applies it.
func (s *Server) handleReload(w http.ResponseWriter, r *http.Request) {
	if err := s.store.Load(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"status": "error",
			"error":  err.Error(),
		})
		return
	}
	if err := s.reload(r.Context()); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"status": "error",
			"error":  err.Error(),
		})
		return
	}
	cfg := s.store.Get()
	writeJSON(w, http.StatusOK, map[string]any{
		"status":          "ok",
		"services_loaded": len(cfg.Services),
	})
}

// handleUpdate accepts a JSON body, overwrites the config file, and reloads.
func (s *Server) handleUpdate(w http.ResponseWriter, r *http.Request) {
	var cfg config.DynamicConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"status": "error",
			"error":  "invalid JSON: " + err.Error(),
		})
		return
	}

	// Save validates and writes atomically; returns error without modifying
	// in-memory config on failure.
	if err := s.store.Save(cfg); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"status": "error",
			"error":  err.Error(),
		})
		return
	}

	if err := s.reload(r.Context()); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"status": "error",
			"error":  err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":          "ok",
		"services_loaded": len(cfg.Services),
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.Encode(v) //nolint:errcheck
}
