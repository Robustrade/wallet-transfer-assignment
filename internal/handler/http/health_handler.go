package http

import (
	"context"
	"net/http"
)

// Pinger reports whether a dependency (the database) is reachable.
type Pinger interface {
	Ping(ctx context.Context) error
}

// HealthHandler serves liveness and readiness probes.
type HealthHandler struct {
	db Pinger
}

// NewHealthHandler wires the handler. db may be nil (liveness only).
func NewHealthHandler(db Pinger) *HealthHandler {
	return &HealthHandler{db: db}
}

// Live handles GET /healthz — the process is up.
func (h *HealthHandler) Live(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Ready handles GET /readyz — dependencies are reachable.
func (h *HealthHandler) Ready(w http.ResponseWriter, r *http.Request) {
	if h.db != nil {
		if err := h.db.Ping(r.Context()); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unavailable", "detail": "database"})
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}
