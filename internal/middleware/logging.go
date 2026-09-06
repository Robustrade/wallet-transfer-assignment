package middleware

import (
	"log/slog"
	"time"

	"wallet-transfer-assignment/internal/observability"

	"github.com/gin-gonic/gin"
)

func Logger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		rawQuery := c.Request.URL.RawQuery

		observability.DefaultMetrics.IncTotalRequests()

		c.Next()

		latency := time.Since(start)
		status := c.Writer.Status()
		reqID, _ := c.Get(HeaderXRequestID)

		if rawQuery != "" {
			path = path + "?" + rawQuery
		}

		slog.Info("HTTP Request",
			"request_id", reqID,
			"status", status,
			"method", c.Request.Method,
			"path", path,
			"ip", c.ClientIP(),
			"latency_ms", latency.Milliseconds(),
		)
	}
}
