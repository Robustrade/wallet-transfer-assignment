package api

import (
	"log/slog"
	"net/http"
	"time"

	"wallet/internal/constants"
	"wallet/internal/dto"
)

// withRequestLogging logs one line per request with method, path, status, and
// duration.
func withRequestLogging(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		logger.InfoContext(r.Context(), "http request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"duration_ms", time.Since(start).Milliseconds())
	})
}

// withRecovery converts a panic in any handler into a 500, so one bad request
// cannot take the server down.
func withRecovery(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				logger.ErrorContext(r.Context(), "panic in handler",
					"method", r.Method, "path", r.URL.Path, "panic", v)
				writeJSON(w, http.StatusInternalServerError,
					dto.NewErrorResponse(constants.CodeInternal, "internal error"))
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// statusRecorder captures the response status for logging.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}
