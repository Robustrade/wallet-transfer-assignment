package logger

import (
	"context"
	"log/slog"
)

type correlationIDKey struct{}

// WithCorrelationID returns a context carrying id, retrievable via
// CorrelationIDFromContext. The only writer of this key is
// middleware.CorrelationID; everything else only ever reads it, which is
// why the setter and getter live together here rather than in the
// middleware package — a service or handler needing the current
// correlation ID for logging has no reason to depend on Gin.
func WithCorrelationID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, correlationIDKey{}, id)
}

// CorrelationIDFromContext returns the correlation ID set by
// WithCorrelationID, or "" if none is present.
func CorrelationIDFromContext(ctx context.Context) string {
	if id, ok := ctx.Value(correlationIDKey{}).(string); ok {
		return id
	}
	return ""
}

// FromContext returns base with the request's correlation ID attached
// as a field, or base unchanged if none is present.
func FromContext(ctx context.Context, base *slog.Logger) *slog.Logger {
	if id := CorrelationIDFromContext(ctx); id != "" {
		return base.With(slog.String("correlation_id", id))
	}
	return base
}
