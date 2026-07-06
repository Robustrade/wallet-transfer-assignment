package middleware

import (
	"time"

	"log/slog"

	"github.com/gin-gonic/gin"
)

// Logger implements a structured JSON logger using slog for observability
func Logger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path

		// Process request
		c.Next()

		// Calculate latency
		latency := time.Since(start)

		// Grab the Request ID we created in the other middleware
		reqID := c.GetString(RequestIDKey)

		// Create the structured log
		slog.Info("HTTP Request",
			slog.String("request_id", reqID),
			slog.String("method", c.Request.Method),
			slog.String("path", path),
			slog.Int("status", c.Writer.Status()),
			slog.String("client_ip", c.ClientIP()),
			slog.Duration("latency", latency),
			slog.String("error-messages", c.Errors.ByType(gin.ErrorTypePrivate).String()),
		)
	}
}
