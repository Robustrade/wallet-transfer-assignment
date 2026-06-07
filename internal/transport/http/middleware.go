package http

import (
	"log/slog"
	"net/http"
	"time"
)

// statusRecorder captures the response status code for access logging.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// middleware is a standard HTTP decorator.
type middleware func(http.Handler) http.Handler

// requestLogMiddleware emits one structured access-log line per request,
// providing basic observability for the API.
func requestLogMiddleware(logger *slog.Logger) middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rec, r)
			logger.InfoContext(r.Context(), "http request",
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", rec.status),
				slog.Duration("duration", time.Since(start)/time.Millisecond),
			)
		})
	}
}

// recoverMiddleware turns a panic in any handler into a 500 response instead of
// crashing the server, and logs the cause.
func recoverMiddleware(logger *slog.Logger) middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					logger.ErrorContext(r.Context(), "panic recovered",
						slog.Any("panic", rec),
						slog.String("path", r.URL.Path),
					)
					writeJSON(w, http.StatusInternalServerError, errorResponse{Error: errorBody{
						Code:    "INTERNAL_ERROR",
						Message: "an unexpected error occurred",
					}})
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}
