package handler

import (
	"wallet-transfer-assignment/internal/middleware"

	"github.com/gin-gonic/gin"
)

func NewRouter(
	transferHandler *TransferHandler,
	walletHandler *WalletHandler,
	healthHandler *HealthHandler,
) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()

	// Apply Middlewares
	router.Use(middleware.RequestID())
	router.Use(middleware.Logger())
	router.Use(middleware.Recovery())

	// Health Endpoints
	router.GET("/health/live", healthHandler.Liveness)
	router.GET("/health/ready", healthHandler.Readiness)

	// API Routes
	router.POST("/transfers", transferHandler.CreateTransfer)
	router.GET("/transfers/:id", transferHandler.GetTransfer)

	router.POST("/wallets", walletHandler.CreateWallet)
	router.GET("/wallets/:id/balance", walletHandler.GetWalletBalance)
	router.GET("/wallets/:id/transactions", walletHandler.GetWalletTransactions)

	return router
}
