package observability

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"log/slog"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestNewMetrics_CollectsMetrics(t *testing.T) {
	registry := prometheus.NewRegistry()
	metrics := NewMetrics(registry)

	metrics.IncIdempotencyHit()
	metrics.ObserveTransferDuration(0.123)
	metrics.IncError("transfer_execute")
	metrics.IncRequest("POST", "/transfers", http.StatusCreated)
	metrics.ObserveRequestDuration("POST", "/transfers", 0.456)

	if got := testutil.ToFloat64(metrics.idempotencyHit); got != 1 {
		t.Fatalf("expected 1 idempotency hit, got %f", got)
	}
	if got := testutil.ToFloat64(metrics.errorCount.WithLabelValues("transfer_execute")); got != 1 {
		t.Fatalf("expected 1 transfer_execute error, got %f", got)
	}
	if got := testutil.ToFloat64(metrics.requestCount.WithLabelValues("POST", "/transfers", "Created")); got != 1 {
		t.Fatalf("expected 1 HTTP request metric, got %f", got)
	}
	if got := testutil.CollectAndCount(metrics.transferDuration); got != 1 {
		t.Fatalf("expected 1 transfer duration observation, got %d", got)
	}
}

func TestHTTPMiddleware_RecordsMetrics(t *testing.T) {
	registry := prometheus.NewRegistry()
	metrics := NewMetrics(registry)
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))

	handler := HTTPMiddleware(logger, metrics)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte("teapot"))
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if got := testutil.ToFloat64(metrics.requestCount.WithLabelValues("GET", "/test", "I'm a teapot")); got != 1 {
		t.Fatalf("expected 1 request count metric, got %f", got)
	}
	if got := testutil.ToFloat64(metrics.errorCount.WithLabelValues("http_request")); got != 1 {
		t.Fatalf("expected 1 HTTP error metric, got %f", got)
	}
}
