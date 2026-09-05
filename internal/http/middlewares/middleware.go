package middlewares

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"wallet-transfer/internal/metrics"
)

const (
	RequestIDHeader = "X-Request-ID"
	RequestIDKey    = "requestId"
)

func RequestID(c *gin.Context) string {
	if id := c.GetString(RequestIDKey); id != "" {
		return id
	}
	return c.GetHeader(RequestIDHeader)
}

func RequestContext() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := c.GetHeader(RequestIDHeader)
		if requestID == "" {
			requestID = uuid.NewString()
		}
		c.Header(RequestIDHeader, requestID)
		c.Set(RequestIDKey, requestID)
		c.Next()
	}
}

func RequestLogging() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		slog.Info("request served",
			"requestId", RequestID(c),
			"method", c.Request.Method,
			"route", RouteOf(c),
			"path", c.Request.URL.Path,
			"status", c.Writer.Status(),
			"durationMs", time.Since(start).Milliseconds(),
		)
	}
}

func RequestMetrics(m *metrics.Metrics) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		m.ObserveRequest(RouteOf(c), c.Request.Method, c.Writer.Status(), time.Since(start))
	}
}

func Timeout(d time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		if d <= 0 {
			c.Next()
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), d)
		defer cancel()

		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}

func Recovery() gin.HandlerFunc {
	return gin.CustomRecovery(func(c *gin.Context, recovered any) {
		slog.Error("panic recovered",
			"requestId", RequestID(c),
			"panic", recovered,
			"path", c.Request.URL.Path,
		)

		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{
				"code":    "INTERNAL_ERROR",
				"message": "an unexpected error occurred",
			},
		})
	})
}

func RouteOf(c *gin.Context) string {
	if route := c.FullPath(); route != "" {
		return route
	}
	return "unmatched"
}
