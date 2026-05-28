package handler

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// --- RequestID -----------------------------------------------------

// TestRequestID_GeneratesWhenMissing verifies that when no inbound
// X-Request-Id header is supplied, the middleware generates a UUID,
// attaches it to the response header, and threads it into the
// request context so downstream code (handlers, the access log
// middleware, the panic-recover middleware) can read the same id.
func TestRequestID_GeneratesWhenMissing(t *testing.T) {
	var seen string
	h := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = RequestIDFromContext(r.Context())
	}))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))

	if seen == "" {
		t.Fatal("expected generated request id")
	}
	if got := w.Header().Get(requestIDHeader); got != seen {
		t.Fatalf("response header %q != context %q", got, seen)
	}
	if len(seen) < 16 {
		t.Fatalf("id looks too short: %q", seen)
	}
}

// TestRequestID_PreservesInbound verifies the trust-upstream branch:
// when an X-Request-Id arrives from an upstream proxy or SDK, the
// middleware uses it as-is rather than generating a fresh one. This
// is what makes cross-service correlation work in a deployment with
// multiple hops.
func TestRequestID_PreservesInbound(t *testing.T) {
	var seen string
	h := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = RequestIDFromContext(r.Context())
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(requestIDHeader, "client-supplied-123")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if seen != "client-supplied-123" {
		t.Fatalf("expected inbound id preserved, got %q", seen)
	}
	if got := w.Header().Get(requestIDHeader); got != "client-supplied-123" {
		t.Fatalf("response header: %q", got)
	}
}

// TestRequestIDFromContext_AbsentReturnsEmpty checks the
// non-HTTP-path safety net: code that looks up the request id from
// a context where no middleware ever ran (e.g. a background job)
// must get "" rather than panicking.
func TestRequestIDFromContext_AbsentReturnsEmpty(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	if id := RequestIDFromContext(r.Context()); id != "" {
		t.Fatalf("expected empty, got %q", id)
	}
}

// --- AccessLog -----------------------------------------------------

// TestAccessLog_EmitsStructuredLine is the contract test for the
// per-request log line. It captures slog output into a buffer,
// drives a request, and verifies the emitted JSON has the expected
// fields. If anyone changes the message ("http_request") or removes
// a field, log aggregator dashboards built on this schema would
// break — this test is the early warning.
func TestAccessLog_EmitsStructuredLine(t *testing.T) {
	buf := captureSlog(t)

	h := Chain(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte("ok"))
	}), RequestID, AccessLog)

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/coffee", nil))

	var line map[string]any
	if err := json.Unmarshal([]byte(firstNonEmpty(buf.String())), &line); err != nil {
		t.Fatalf("could not parse log line: %v -- raw: %s", err, buf.String())
	}
	if line["msg"] != "http_request" {
		t.Fatalf("msg: %v", line["msg"])
	}
	if line["method"] != "GET" {
		t.Fatalf("method: %v", line["method"])
	}
	if line["path"] != "/coffee" {
		t.Fatalf("path: %v", line["path"])
	}
	if int(line["status"].(float64)) != http.StatusTeapot {
		t.Fatalf("status: %v", line["status"])
	}
	if _, ok := line["duration_ms"]; !ok {
		t.Fatal("missing duration_ms")
	}
	if line["request_id"] == "" {
		t.Fatal("missing request_id")
	}
}

// TestAccessLog_DefaultsStatusTo200 covers the statusRecorder
// implicit-200 case: handlers that call Write without first calling
// WriteHeader still produce a 200 response (net/http behaviour) and
// the access log must report 200, not 0.
func TestAccessLog_DefaultsStatusTo200(t *testing.T) {
	buf := captureSlog(t)
	h := AccessLog(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("hi")) // no explicit WriteHeader
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	if !strings.Contains(buf.String(), `"status":200`) {
		t.Fatalf("expected status 200 in log: %s", buf.String())
	}
}

// --- Recover -------------------------------------------------------

// TestRecover_CatchesPanicAndReturns500 verifies the safety-net
// behaviour: a panic from any downstream handler must NOT crash the
// goroutine; the client must see a generic 500 with the documented
// JSON shape; and the server-side log must capture the panic value
// plus a stack trace so operators can debug.
func TestRecover_CatchesPanicAndReturns500(t *testing.T) {
	buf := captureSlog(t)
	h := Chain(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		panic("kaboom")
	}), RequestID, Recover)

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/oops", nil))

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status: %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), `"error":"internal error"`) {
		t.Fatalf("body: %s", w.Body.String())
	}
	if !strings.Contains(buf.String(), "panic recovered") {
		t.Fatalf("log missing panic recovered: %s", buf.String())
	}
	if !strings.Contains(buf.String(), "kaboom") {
		t.Fatalf("log missing panic value: %s", buf.String())
	}
}

// TestRecover_PassesThroughOnNoPanic checks the boring happy path:
// the middleware must be transparent when nothing panics, including
// preserving the status code the inner handler chose.
func TestRecover_PassesThroughOnNoPanic(t *testing.T) {
	h := Recover(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if w.Code != http.StatusAccepted {
		t.Fatalf("status: %d", w.Code)
	}
}

// --- Chain ---------------------------------------------------------

// TestChain_OrderIsOutermostFirst pins down the documented
// composition order: in Chain(h, A, B, C), middleware A runs first
// on the way in and last on the way out. This matters because the
// rest of the codebase assumes that order — for example RequestID
// must run before AccessLog so the log line has the id available.
//
// The test records enter/exit events from three trace middlewares
// and asserts the exact sequence.
func TestChain_OrderIsOutermostFirst(t *testing.T) {
	var order []string
	mark := func(name string) func(http.Handler) http.Handler {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				order = append(order, "enter:"+name)
				next.ServeHTTP(w, r)
				order = append(order, "exit:"+name)
			})
		}
	}
	h := Chain(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		order = append(order, "handler")
	}), mark("a"), mark("b"), mark("c"))

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	want := []string{"enter:a", "enter:b", "enter:c", "handler", "exit:c", "exit:b", "exit:a"}
	if len(order) != len(want) {
		t.Fatalf("order length: got %v want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order[%d]: got %s want %s (full %v)", i, order[i], want[i], order)
		}
	}
}

// --- helpers -------------------------------------------------------

// captureSlog redirects the default slog logger to a buffer for the
// duration of the test and returns the buffer for assertion.
func captureSlog(t *testing.T) *bytes.Buffer {
	t.Helper()
	buf := &bytes.Buffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return buf
}

func firstNonEmpty(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if line != "" {
			return line
		}
	}
	return ""
}
