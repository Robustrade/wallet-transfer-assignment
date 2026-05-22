package idempotency

import (
	handlers "wallet-transfer-assignment/internal/handlers/idempotency"

	"github.com/gin-gonic/gin"
)

func RegisterRoutes(rg *gin.RouterGroup, handler handlers.IdempotencyHandler) {
	idempotencyRouterGroup := rg.Group("")

	idempotencyRouterGroup.POST("/generate-id", handler.GenerateID)
}
