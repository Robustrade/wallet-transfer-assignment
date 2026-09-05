package http

import (
	"github.com/gin-gonic/gin"

	"wallet-transfer/internal/http/handlers"
)

func NewRouter(transfers handlers.TransferService) *gin.Engine {
	engine := gin.Default()

	handler := handlers.NewTransferHandler(transfers)
	engine.POST("/transfers", handler.Create)

	return engine
}
