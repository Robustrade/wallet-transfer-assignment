package controller

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Robustrade/wallet-transfer-assignment/internal/database"
)

func HealthCheck(c *gin.Context) {
	sqlDB, err := database.DB.DB()
	if err == nil {
		err = sqlDB.PingContext(c.Request.Context())
	}
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"status": "database unavailable"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}
