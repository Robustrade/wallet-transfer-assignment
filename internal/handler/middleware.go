package handler

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
)

type contextKey string

const RequestIDKey contextKey = "request_id"

type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// RequestLogger logs all incoming HTTP requests with latency, status code, and request ID.
func RequestLogger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			requestID := r.Header.Get("X-Request-ID")
			if requestID == "" {
				requestID = uuid.New().String()
			}

			ctx := context.WithValue(r.Context(), RequestIDKey, requestID)
			w.Header().Set("X-Request-ID", requestID)

			rw := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

			defer func() {
				duration := time.Since(start)
				logger.InfoContext(ctx, "http_request",
					slog.String("request_id", requestID),
					slog.String("method", r.Method),
					slog.String("path", r.URL.Path),
					slog.Int("status", rw.statusCode),
					slog.Duration("latency", duration),
					slog.String("remote_addr", r.RemoteAddr),
				)
			}()

			next.ServeHTTP(rw, r.WithContext(ctx))
		})
	}
}

// CORSMiddleware adds CORS headers to allow the UI to call the API from the browser.
func CORSMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Request-ID")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RecoveryMiddleware catches unhandled panics and returns a 500 error cleanly.
func RecoveryMiddleware(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					logger.Error("unhandled panic recovered", slog.Any("panic", rec))
					WriteError(w, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "an unexpected error occurred")
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}
