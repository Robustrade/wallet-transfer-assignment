package transfer

import (
	handlers "wallet-transfer-assignment/internal/handlers/transfer"
	"wallet-transfer-assignment/internal/middlewares"
	"wallet-transfer-assignment/internal/services/cache"

	"github.com/gin-gonic/gin"
)

func RegisterRoutes(rg *gin.RouterGroup, handler handlers.TransferHandler, cacheService cache.CacheHelper) {
	transferRouterGroup := rg.Group("")

	transferRouterGroup.POST("/transfers", middlewares.IdempotencyMiddleware(cacheService), handler.CreateTransfer)
}
