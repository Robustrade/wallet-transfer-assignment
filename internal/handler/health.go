package handler

import (
	"context"
	"net/http"
	"time"
)

// handleHealthz is a liveness probe: the process is up and serving.
func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	respondJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleReadyz is a readiness probe: the service can reach its database.
func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.pool.Ping(ctx); err != nil {
		respondError(w, http.StatusServiceUnavailable, "not ready")
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (s *Server) handleNotFound(w http.ResponseWriter, _ *http.Request) {
	respondError(w, http.StatusNotFound, "not found")
}

func (s *Server) handleMethodNotAllowed(w http.ResponseWriter, _ *http.Request) {
	respondError(w, http.StatusMethodNotAllowed, "method not allowed")
}
