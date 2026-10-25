package handler

import (
	"database/sql"
	"net/http"
	"time"
)

type HealthHandler struct {
	db *sql.DB
}

func NewHealthHandler(db *sql.DB) *HealthHandler {
	return &HealthHandler{db: db}
}

func (h *HealthHandler) HealthCheck(w http.ResponseWriter, r *http.Request) {
	dbStatus := "UP"
	if h.db != nil {
		if err := h.db.PingContext(r.Context()); err != nil {
			dbStatus = "DOWN: " + err.Error()
		}
	}

	WriteJSON(w, http.StatusOK, map[string]any{
		"status":    "UP",
		"database":  dbStatus,
		"timestamp": time.Now().UTC(),
	})
}
