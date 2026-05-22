package handlers

import "github.com/gin-gonic/gin"

type TransferHandler interface {
	CreateTransfer(c *gin.Context)
}
