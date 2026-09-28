package middleware

import (
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
)

// Logger writes exactly one structured log line per request, after the
// response is complete. Transfer-specific fields (idempotency key, transfer
// id, outcome) are set by the handler through the gin context. Request bodies
// are never logged.
func Logger(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		status := c.Writer.Status()
		attrs := []slog.Attr{
			slog.String("request_id", c.GetString(KeyRequestID)),
			slog.String("method", c.Request.Method),
			slog.String("route", c.FullPath()), // the pattern, e.g. /wallets/:id, not the raw path
			slog.Int("status", status),
			slog.Float64("duration_ms", float64(time.Since(start).Microseconds())/1000),
		}
		for _, key := range []string{KeyIdempotencyKey, KeyTransferID} {
			if v := c.GetString(key); v != "" {
				attrs = append(attrs, slog.String(key, v))
			}
		}
		outcome := c.GetString(KeyOutcome)
		if outcome == "" && status >= 400 {
			outcome = "error"
		}
		if outcome != "" {
			attrs = append(attrs, slog.String("outcome", outcome))
		}
		if v := c.GetString(KeyError); v != "" {
			attrs = append(attrs, slog.String("error", v))
		}

		level := slog.LevelInfo
		if status >= 500 {
			level = slog.LevelError
		}
		logger.LogAttrs(c.Request.Context(), level, "request", attrs...)
	}
}
