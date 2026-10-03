package handler

import (
	"net/http"

	"github.com/labstack/echo/v4"
)

// HealthHandler provides health check endpoints.
type HealthHandler struct {
	serviceName string
}

// NewHealthHandler constructs a HealthHandler.
func NewHealthHandler(serviceName string) *HealthHandler {
	return &HealthHandler{serviceName: serviceName}
}

// Check handles GET /health.
func (h *HealthHandler) Check(c echo.Context) error {
	return c.JSON(http.StatusOK, echo.Map{
		"status":  "ok",
		"service": h.serviceName,
	})
}
