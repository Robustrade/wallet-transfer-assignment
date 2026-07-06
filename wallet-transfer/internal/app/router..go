package app

import (
	"wallet-transfer/middleware"

	"github.com/gin-gonic/gin"
)

// SetupRoutes maps the HTTP endpoints to the internal handlers
func (app *Application) SetupRouter(r *gin.Engine) {
	// Global Middleware
	r.Use(middleware.RequestID())
	r.Use(middleware.Logger())
	r.Use(gin.Recovery())

	v1 := r.Group("/v1")

	// Authenticated Group
	authGroup := v1.Group("")
	{
		authGroup.Use(middleware.Authentication())
		authGroup.Use(middleware.Authorization())

		// --- Transfers Domain ---
		transfers := authGroup.Group("/transfers")
		{
			transfers.POST("", app.transferHandler.CreateTransfer)
		}

		// --- Wallets Domain ---
		wallets := authGroup.Group("/wallets")
		{
			wallets.GET("/:id/balance", app.walletHandler.GetBalance)
		}
	}
}
