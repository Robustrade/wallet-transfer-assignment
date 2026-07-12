// Package handler is the thin HTTP layer: decode request, call service,
// encode response. No business logic here — every domain rule lives in
// service/domain so it's testable independent of HTTP.
package handler

import (
	"github.com/gin-gonic/gin"

	"github.com/Nandukrishna-S/wallet-transfer-assignment/internal/middleware"
)

// NewRouter registers all route groups and their middleware chains.
// /health is deliberately dependency-free (no middleware group) so it
// keeps working even if correlation-id middleware itself failed to init.
func NewRouter(healthHandler *HealthHandler, transferHandler *TransferHandler) *gin.Engine {
	router := gin.New()
	router.Use(gin.Recovery())

	router.GET("/health", healthHandler.Health)

	transfers := router.Group("/transfers")
	transfers.Use(middleware.CorrelationID())
	{
		transfers.POST("/init", transferHandler.InitTransfer)
		transfers.POST("", transferHandler.CreateTransfer)
		transfers.GET("/:id", transferHandler.GetTransfer)
	}

	return router
}
