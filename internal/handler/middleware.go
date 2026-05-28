package handler

import (
	"context"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/google/uuid"
)

// requestIDHeader is the canonical header used both for inbound
// correlation ids (forwarded from upstream proxies or SDKs) and for
// the id we attach to every response.
const requestIDHeader = "X-Request-Id"

// ctxKey is a private type for context keys. Defining it as an
// unexported type prevents accidental collisions with keys set by
// other packages (Go encourages this pattern explicitly in the
// context package docs).
type ctxKey int

const (
	// requestIDKey is the context key under which the RequestID
	// middleware stores the per-request id. Read it back with
	// RequestIDFromContext.
	requestIDKey ctxKey = iota
)

// RequestIDFromContext returns the request id attached by the
// RequestID middleware, or "" if there is none (e.g. when called from
// non-HTTP code). It never panics.
func RequestIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(requestIDKey).(string); ok {
		return v
	}
	return ""
}

// RequestID middleware ensures every request has a correlation id.
//
// If the inbound request carries an X-Request-Id header (e.g. set by
// an upstream load balancer or by the client SDK) we trust it and
// thread it through. Otherwise we generate a UUID. Either way the id
// is reflected on the response so clients can quote it back to support
// without us having to dig through logs first.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(requestIDHeader)
		if id == "" {
			id = uuid.NewString()
		}
		w.Header().Set(requestIDHeader, id)
		ctx := context.WithValue(r.Context(), requestIDKey, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// statusRecorder wraps an http.ResponseWriter so the access log can
// observe the final status code chosen by the handler.
//
// We default the status to 200 because handlers that call Write
// without first calling WriteHeader still produce a 200 response —
// that's the standard library behaviour, and we should not log a
// different number from what the client actually saw.
//
// The `wrote` flag prevents double-counting: only the first
// WriteHeader call (or the implicit one from Write) determines the
// status; subsequent calls would normally trigger a "superfluous
// WriteHeader" warning in net/http and we want our log to match.
type statusRecorder struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (s *statusRecorder) WriteHeader(code int) {
	if !s.wrote {
		s.status = code
		s.wrote = true
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if !s.wrote {
		s.status = http.StatusOK
		s.wrote = true
	}
	return s.ResponseWriter.Write(b)
}

// AccessLog emits a single structured log line per request.
//
// The line is JSON (matches the default logger configured in
// cmd/server) and carries: request_id, method, path, status,
// duration_ms. Bodies are NEVER logged — see AGENTS.md §S-6.
//
// The middleware times from the moment the handler is called to the
// moment it returns, so the duration includes any work the handler
// did but excludes the time the connection spent waiting on the
// network.
func AccessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		slog.LogAttrs(r.Context(), slog.LevelInfo, "http_request",
			slog.String("request_id", RequestIDFromContext(r.Context())),
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.Int("status", rec.status),
			slog.Int64("duration_ms", time.Since(start).Milliseconds()),
		)
	})
}

// Recover is the last line of defence. Any panic from a downstream
// handler or middleware is caught here, logged with a full stack
// trace, and translated to a 500 with a generic JSON body so the
// client never sees a goroutine dump and never has the connection
// torn at the TCP layer.
//
// The request id is included in the log so the failure can be
// correlated with the client report. The full panic value is logged
// server-side only.
//
// We deliberately do NOT re-panic. The point of this middleware is to
// keep the server up; a re-raise would propagate to net/http's
// connection-level handler which logs the goroutine dump to stdout —
// useful when there is no Recover, redundant when there is one.
func Recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				slog.ErrorContext(r.Context(), "panic recovered",
					slog.Any("panic", rec),
					slog.String("stack", string(debug.Stack())),
					slog.String("request_id", RequestIDFromContext(r.Context())),
					slog.String("method", r.Method),
					slog.String("path", r.URL.Path),
				)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"error":"internal error"}`))
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// Chain composes middlewares around an http.Handler.
//
// The first argument in `mws` is the OUTERMOST wrapper — it runs first
// on the way in and last on the way out, matching the natural reading
// order of `Chain(mux, A, B, C)` as "A wraps B wraps C wraps mux".
//
// Recommended order in this service: RequestID -> AccessLog -> Recover
// so that:
//   - request_id is in the context before any logging happens,
//   - access log sees the final status (including 500s from Recover),
//   - panics are caught before reaching the listener.
func Chain(h http.Handler, mws ...func(http.Handler) http.Handler) http.Handler {
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	return h
}
