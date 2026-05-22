package handlers

import "github.com/gin-gonic/gin"

type IdempotencyHandler interface {
	GenerateID(c *gin.Context)
}
