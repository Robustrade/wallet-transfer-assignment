package handler

import (
	"context"
	"net/http"
	"time"

	"github.com/Robustrade/wallet-transfer-assignment/internal/repository"
)

// HealthHandler provides health check endpoints for liveness and readiness monitoring.
type HealthHandler struct {
	repo repository.WalletRepository
}

// NewHealthHandler constructs a HealthHandler.
func NewHealthHandler(repo repository.WalletRepository) *HealthHandler {
	return &HealthHandler{repo: repo}
}

// HandleHealthz processes GET /healthz
func (h *HealthHandler) HandleHealthz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	if err := h.repo.Ping(ctx); err != nil {
		WriteJSON(w, http.StatusServiceUnavailable, map[string]string{
			"status":   "unhealthy",
			"database": "disconnected",
		})
		return
	}

	WriteJSON(w, http.StatusOK, map[string]string{
		"status":   "healthy",
		"database": "connected",
	})
}
