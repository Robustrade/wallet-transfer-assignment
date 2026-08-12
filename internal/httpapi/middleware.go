package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/google/uuid"
)

type ctxKey int

const loggerCtxKey ctxKey = iota

// loggerFromContext returns the request-scoped logger installed by
// withRequestID, or slog.Default() if none is present (e.g. in a unit test
// that calls a handler directly without going through the middleware
// chain).
func loggerFromContext(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(loggerCtxKey).(*slog.Logger); ok {
		return l
	}
	return slog.Default()
}

// withMiddleware wraps next with, from outermost to innermost: panic
// recovery, the access log, and request id assignment. Request bodies and
// wallet balances are deliberately never logged here — see docs/design.md
// §10.
func withMiddleware(next http.Handler, logger *slog.Logger) http.Handler {
	return recoverPanic(accessLog(withRequestID(next), logger), logger)
}

func withRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-Id")
		if id == "" {
			id = uuid.NewString()
		}
		w.Header().Set("X-Request-Id", id)

		scoped := slog.Default().With("request_id", id)
		ctx := context.WithValue(r.Context(), loggerCtxKey, scoped)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func accessLog(next http.Handler, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		logger.Info("http_request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"duration_ms", time.Since(start).Milliseconds(),
			"request_id", w.Header().Get("X-Request-Id"),
		)
	})
}

func recoverPanic(next http.Handler, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				logger.Error("panic recovered", "panic", rec, "stack", string(debug.Stack()))
				writeJSON(w, http.StatusInternalServerError, errorResponse{Error: errorBody{Code: "INTERNAL_ERROR", Message: "an internal error occurred"}})
			}
		}()
		next.ServeHTTP(w, r)
	})
}
