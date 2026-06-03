package router

import (
	"wallet-transfer/internal/handler"

	"github.com/gin-gonic/gin"
)

func Setup(h *handler.TransferHandler) *gin.Engine {
	r := gin.Default()

	r.POST("/transfers", h.CreateTransfer)
	r.POST("/wallets", h.CreateWallet)
	r.GET("/wallets/:id", h.GetWallet)

	return r
}
