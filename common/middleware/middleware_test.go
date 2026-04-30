package middleware_test

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	apperr "github.com/Robustrade/wallet-transfer-assignment/common/error"
	"github.com/Robustrade/wallet-transfer-assignment/common/middleware"
)

// TestRecovery_HandlerPanic_Returns500 — the Recovery middleware must turn a
// downstream panic into a JSON 500 instead of letting the panic propagate
// (which would leak a connection and crash the goroutine).
func TestRecovery_HandlerPanic_Returns500(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	panicker := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	})
	handler := middleware.RequestID(middleware.Recovery(logger)(panicker))

	req := httptest.NewRequest(http.MethodGet, "/anything", nil)
	rec := httptest.NewRecorder()

	// Should NOT propagate the panic.
	require.NotPanics(t, func() { handler.ServeHTTP(rec, req) })

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Contains(t, rec.Body.String(), "INTERNAL_ERROR")
}

// TestRecovery_LogsPanic — the recovery middleware logs the panic at error
// level with the recovered value and a stack trace; verify by inspecting the
// captured log buffer.
func TestRecovery_LogsPanic(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	panicker := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("kaboom-token")
	})
	handler := middleware.RequestID(middleware.Recovery(logger)(panicker))

	req := httptest.NewRequest(http.MethodGet, "/anything", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	logged := buf.String()
	assert.Contains(t, logged, "panic recovered")
	assert.Contains(t, logged, "kaboom-token", "the recovered value must be in the log line")
	assert.Contains(t, logged, "level=ERROR")
}

// TestRequestID_GeneratesWhenAbsent — when the client doesn't send an
// X-Request-ID, the middleware generates one, echoes it in the response
// header, and threads it through the context so downstream handlers and
// loggers can pick it up.
func TestRequestID_GeneratesWhenAbsent(t *testing.T) {
	var seenInHandler string
	probe := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seenInHandler = middleware.RequestIDFromContext(r.Context())
	})
	handler := middleware.RequestID(probe)

	req := httptest.NewRequest(http.MethodGet, "/anything", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	echoed := rec.Header().Get(middleware.HeaderRequestID)
	assert.NotEmpty(t, echoed, "response must carry an X-Request-ID")
	assert.Equal(t, echoed, seenInHandler, "context value must match response header")
}

// TestRequestID_PropagatesIncoming — when the client supplies an X-Request-ID,
// the middleware MUST keep that value (so distributed traces stitch up across
// services) rather than minting a new one.
func TestRequestID_PropagatesIncoming(t *testing.T) {
	const supplied = "client-supplied-id-1234"

	var seenInHandler string
	probe := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seenInHandler = middleware.RequestIDFromContext(r.Context())
	})
	handler := middleware.RequestID(probe)

	req := httptest.NewRequest(http.MethodGet, "/anything", nil)
	req.Header.Set(middleware.HeaderRequestID, supplied)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, supplied, rec.Header().Get(middleware.HeaderRequestID))
	assert.Equal(t, supplied, seenInHandler)
}

// TestWriteError_IncludesRequestIDInBody — error responses should carry the
// request_id in the JSON body in addition to the X-Request-ID header so a
// client can quote the body verbatim in a bug report and the operator can
// grep server logs by that id.
func TestWriteError_IncludesRequestIDInBody(t *testing.T) {
	const supplied = "trace-abc-123"

	errHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		middleware.WriteError(w, r, apperr.ErrWalletNotFound)
	})
	handler := middleware.RequestID(errHandler)

	req := httptest.NewRequest(http.MethodGet, "/anything", nil)
	req.Header.Set(middleware.HeaderRequestID, supplied)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))

	assert.Equal(t, "WALLET_NOT_FOUND", body["code"])
	assert.Equal(t, supplied, body["request_id"],
		"error body must echo the request_id so clients can quote it in bug reports")
}
