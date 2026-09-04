package middleware

import (
	"log/slog"
	"net/http"

	"wallet-transfer-assignment/internal/domain"

	"github.com/gin-gonic/gin"
)

func Recovery() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if err := recover(); err != nil {
				reqID, _ := c.Get(HeaderXRequestID)
				slog.Error("Panic recovered", "error", err, "request_id", reqID)

				appErr := domain.ErrInternal(nil)
				c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
					"error": appErr,
				})
			}
		}()
		c.Next()
	}
}
