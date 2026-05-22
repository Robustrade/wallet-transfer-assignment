package routes

import (
	idempotencyRoutes "wallet-transfer-assignment/internal/api/routes/idempotency"
	transferRoutes "wallet-transfer-assignment/internal/api/routes/transfer"
	idempotencyHandlers "wallet-transfer-assignment/internal/handlers/idempotency"
	transferHandlers "wallet-transfer-assignment/internal/handlers/transfer"
	"wallet-transfer-assignment/internal/services/cache"
	"wallet-transfer-assignment/internal/services/environment"
	"wallet-transfer-assignment/internal/utils"

	"github.com/gin-gonic/gin"
)

func SetupRouter(
	transferHandler transferHandlers.TransferHandler,
	idempotencyHandler idempotencyHandlers.IdempotencyHandler,
	cacheService cache.CacheHelper,
) *gin.Engine {
	if !utils.GetBoolFromString(environment.GoDotEnvVariable("DEBUG", "true")) {
		gin.SetMode(gin.ReleaseMode)
	}
	r := gin.Default()

	baseRouterGroup := r.Group("")
	idempotencyRoutes.RegisterRoutes(baseRouterGroup, idempotencyHandler)
	transferRoutes.RegisterRoutes(baseRouterGroup, transferHandler, cacheService)

	return r
}
