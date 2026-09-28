package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/LokilokeshM/wallet-transfer-assignment/internal/middleware"
)

type healthHandler struct {
	db Pinger
}

// healthz reports that the process is up. It never touches the database, so
// an orchestrator does not restart a healthy process during a DB outage.
func (h *healthHandler) healthz(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// readyz reports whether the service can serve traffic, which requires a
// reachable database.
func (h *healthHandler) readyz(c *gin.Context) {
	if err := h.db.PingContext(c.Request.Context()); err != nil {
		c.Set(middleware.KeyError, err.Error())
		middleware.AbortWithError(c, http.StatusServiceUnavailable, middleware.CodeUnavailable, "database is not reachable")
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ready"})
}
