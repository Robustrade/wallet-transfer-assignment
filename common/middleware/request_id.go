// Package middleware contains HTTP middlewares for the transfer service.
package middleware

import (
	"context"
	"net/http"

	"github.com/google/uuid"
)

// requestIDKey is the private context key for the request ID.
type requestIDKey struct{}

// HeaderRequestID is the HTTP header used to propagate request IDs.
const HeaderRequestID = "X-Request-ID"

// RequestID is middleware that generates (or propagates) a request ID and
// stores it in the context and response header.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(HeaderRequestID)
		if id == "" {
			id = uuid.NewString()
		}
		w.Header().Set(HeaderRequestID, id)
		ctx := context.WithValue(r.Context(), requestIDKey{}, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequestIDFromContext retrieves the request ID from the context, or "" if absent.
func RequestIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(requestIDKey{}).(string); ok {
		return v
	}
	return ""
}
