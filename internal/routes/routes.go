package routes

import (
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"wallet-transfer-service/internal/handler"
	"wallet-transfer-service/internal/middleware"
)

func SetupRouter(
	walletHandler *handler.WalletHandler,
	transferHandler *handler.TransferHandler,
	allowedOrigins []string,
) *gin.Engine {
	slog.Debug("setting up HTTP router")
	handler.RegisterCustomValidators()
	r := gin.New()

	r.Use(gin.Recovery())
	r.Use(gin.Logger())

	// Perimeter security middleware
	r.Use(middleware.SecurityHeaders())      // Defensive response headers
	r.Use(middleware.MaxBodySize(64 * 1024)) // 64 KB payload limit
	r.Use(middleware.CORS(allowedOrigins))   // Environment-driven CORS configuration

	r.GET("/health", func(c *gin.Context) {
		slog.Info("health check requested")
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// API v1 routes
	v1 := r.Group("/api/v1")
	v1.Use(middleware.EnforceJSONContentType())
	{
		wallets := v1.Group("/wallets")
		{
			wallets.POST("", walletHandler.CreateWallet)
			wallets.GET("/:id", walletHandler.GetWallet)
			wallets.PATCH("/:id/status", walletHandler.UpdateStatus)
			wallets.GET("/:id/statement", walletHandler.GetStatement)
		}

		v1.POST("/transfers", transferHandler.CreateTransfer)
	}

	slog.Info("HTTP router setup completed")
	return r
}
