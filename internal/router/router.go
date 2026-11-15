package router

import (
	"github.com/gin-gonic/gin"

	"github.com/Robustrade/wallet-transfer-assignment/internal/controller"
)

func SetupRoutes(r *gin.Engine) {
	r.GET("/healthz", controller.HealthCheck)

	r.POST("/create-wallet", controller.CreateWallet)

	r.POST("/transfers", controller.CreateTransfer)
}
