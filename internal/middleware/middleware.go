// Package middleware contains cross-cutting HTTP concerns applied by the
// router: request ID correlation, request logging, and panic recovery.
package middleware

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/wallet-transfer-assignment/wallet-transfer/internal/controller"
)

type contextKey string

const requestIDContextKey contextKey = "requestID"

// RequestID assigns a request ID (reusing an inbound X-Request-ID header
// when present) and stores it on the request context and response header
// for correlation across logs.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if id == "" {
			id = uuid.NewString()
		}
		w.Header().Set("X-Request-ID", id)
		ctx := context.WithValue(r.Context(), requestIDContextKey, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// Logging logs each request's method, path, status, duration, and request ID.
func Logging(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rec, r)
			logger.Info("http request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", rec.status,
				"duration_ms", time.Since(start).Milliseconds(),
				"request_id", r.Context().Value(requestIDContextKey),
			)
		})
	}
}

// Recover converts panics in downstream handlers into a 500 response
// instead of crashing the server.
func Recover(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					logger.Error("panic recovered", "error", rec)
					controller.WriteError(w, logger, http.StatusInternalServerError, "internal_error", "an unexpected error occurred")
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}
