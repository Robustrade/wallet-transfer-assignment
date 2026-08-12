package httpapi

import (
	"context"
	"net/http"
)

// HealthChecker is satisfied by the postgres connection pool.
type HealthChecker interface {
	Ping(ctx context.Context) error
}

type systemHandler struct {
	health HealthChecker
}

// Healthz is a pure liveness probe: no I/O, so it can't itself become the
// reason a load balancer marks the instance unhealthy.
func (h *systemHandler) Healthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Readyz additionally checks that the database is reachable.
func (h *systemHandler) Readyz(w http.ResponseWriter, r *http.Request) {
	if err := h.health.Ping(r.Context()); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unavailable"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
