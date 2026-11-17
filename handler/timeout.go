package handler

import (
	"context"
	"net/http"
	"time"
)

// WithRequestTimeout cancels the request context after d so pool acquire and DB
// work cannot wait indefinitely when the connection pool is saturated.
func WithRequestTimeout(d time.Duration, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), d)
		defer cancel()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
