package http

import (
	"github.com/gin-gonic/gin"

	"wallet-transfer/internal/http/handlers"
)

func NewRouter(transfers handlers.TransferService, wallets handlers.WalletService) *gin.Engine {
	engine := gin.Default()

	transferHandler := handlers.NewTransferHandler(transfers)
	walletHandler := handlers.NewWalletHandler(wallets)

	engine.POST("/transfers", transferHandler.Create)
	engine.GET("/transfers/:id", transferHandler.Get)
	engine.GET("/wallets/:id", walletHandler.Get)
	engine.GET("/wallets/:id/transfers", walletHandler.GetHistory)

	return engine
}
