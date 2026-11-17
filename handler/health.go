package handler

import "net/http"

// health responds to liveness probes. It does not touch the database.
func (h *Handler) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
