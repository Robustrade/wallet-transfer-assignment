// Package middleware holds cross-cutting HTTP concerns. Attached per
// route-group, not globally — there's currently no concern that applies
// to literally every route (e.g. /health deliberately stays
// dependency-free).
package middleware

import (
	"context"
	"log/slog"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type correlationIDKey struct{}

const correlationIDHeader = "X-Correlation-ID"

// CorrelationID attaches a per-request correlation ID to the request
// context, reusing an inbound X-Correlation-ID header if present and
// echoing it back on the response. This is the one sanctioned use of
// context.Value here: request-scoped data only, never app-scoped
// dependencies like a DB pool, which are constructor-injected instead.
func CorrelationID() gin.HandlerFunc {
	return func(c *gin.Context) {
		correlationID := c.GetHeader(correlationIDHeader)
		if correlationID == "" {
			correlationID = uuid.NewString()
		}

		ctx := context.WithValue(c.Request.Context(), correlationIDKey{}, correlationID)
		c.Request = c.Request.WithContext(ctx)

		c.Header(correlationIDHeader, correlationID)
		c.Next()
	}
}

// CorrelationIDFromContext returns the correlation ID set by CorrelationID,
// or "" if none is present.
func CorrelationIDFromContext(ctx context.Context) string {
	if id, ok := ctx.Value(correlationIDKey{}).(string); ok {
		return id
	}
	return ""
}

// LoggerWithCorrelationID returns logger with the request's correlation ID
// attached as a field, or logger unchanged if none is present.
func LoggerWithCorrelationID(ctx context.Context, logger *slog.Logger) *slog.Logger {
	if id := CorrelationIDFromContext(ctx); id != "" {
		return logger.With(slog.String("correlation_id", id))
	}
	return logger
}
