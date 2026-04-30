package middleware

import (
	"log/slog"
	"net/http"
	"runtime/debug"

	apperr "github.com/Robustrade/wallet-transfer-assignment/common/error"
)

// Recovery returns middleware that catches panics in downstream handlers,
// logs them with the stack trace, and writes a 500 JSON response.
//
// A nil logger falls back to slog.Default() so the middleware never panics
// while handling a panic — that would leak the connection and skip the 500
// response we still owe the client.
func Recovery(logger *slog.Logger) func(http.Handler) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					logger.ErrorContext(
						r.Context(),
						"panic recovered",
						"panic", rec,
						"stack", string(debug.Stack()),
						"request_id", RequestIDFromContext(r.Context()),
					)
					WriteError(w, r, apperr.ErrInternal)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}
