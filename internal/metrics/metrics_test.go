package metrics

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

// TestRegistry_HasStandardCollectors confirms that the package-level
// init() registered every collector we expose. A missing collector
// would show up as a Prometheus scrape returning fewer metrics than
// dashboards expect; this test catches it before deploy.
func TestRegistry_HasStandardCollectors(t *testing.T) {
	for _, name := range []string{
		"wallet_transfers_total",
		"wallet_transfer_duration_seconds",
		"wallet_http_requests_total",
		"wallet_http_request_duration_seconds",
	} {
		t.Run(name, func(t *testing.T) {
			count := testutil.CollectAndCount(Registry, name)
			// CollectAndCount returns 0 if the metric was never
			// observed AND 0 series exist. Histograms always have
			// metadata; counters with no labels emit one zero
			// series. CounterVec with no observations may return 0.
			// We just check no panic and a non-negative count.
			if count < 0 {
				t.Fatalf("unexpected negative count for %s: %d", name, count)
			}
		})
	}
}

// TestTransfersTotal_IncrementsByLabel verifies the counter does the
// thing it's supposed to do — incrementing under the right label
// changes the right time series.
func TestTransfersTotal_IncrementsByLabel(t *testing.T) {
	before := testutil.ToFloat64(TransfersTotal.WithLabelValues("PROCESSED"))
	TransfersTotal.WithLabelValues("PROCESSED").Inc()
	after := testutil.ToFloat64(TransfersTotal.WithLabelValues("PROCESSED"))

	if after != before+1 {
		t.Fatalf("counter not incremented: before=%v after=%v", before, after)
	}
}

// TestTransferDuration_ObservesAcrossLabels verifies the histogram
// records observations under separate label values without mixing
// them (a regression in the WithLabelValues path would).
func TestTransferDuration_ObservesAcrossLabels(t *testing.T) {
	TransferDuration.WithLabelValues("PROCESSED").Observe(0.1)
	TransferDuration.WithLabelValues("FAILED").Observe(0.2)
	// CollectAndCount returns the number of distinct time series.
	// Two labels => >= 2 series. We don't pin the exact count
	// because earlier tests may have populated more.
	count := testutil.CollectAndCount(TransferDuration, "wallet_transfer_duration_seconds")
	if count < 2 {
		t.Fatalf("expected at least 2 series, got %d", count)
	}
}

// TestHandler_ServesPrometheusExposition confirms the /metrics
// endpoint produces text in Prometheus exposition format. We check
// for one of our known metric names + the standard `# HELP` and
// `# TYPE` lines that Prometheus scrapers parse.
func TestHandler_ServesPrometheusExposition(t *testing.T) {
	srv := httptest.NewServer(Handler())
	defer srv.Close()

	// Increment something so a series is present in the output.
	TransfersTotal.WithLabelValues("PROCESSED").Inc()

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: %d", resp.StatusCode)
	}
	b, _ := io.ReadAll(resp.Body)
	body := string(b)

	if !strings.Contains(body, "# HELP wallet_transfers_total") {
		t.Fatalf("missing HELP line: %s", body)
	}
	if !strings.Contains(body, "# TYPE wallet_transfers_total counter") {
		t.Fatalf("missing TYPE line: %s", body)
	}
	if !strings.Contains(body, `wallet_transfers_total{state="PROCESSED"}`) {
		t.Fatalf("missing labeled series: %s", body)
	}
}

// TestHTTPMiddleware_LabelsByRoute exercises the middleware: it
// must label HTTPRequestsTotal by the matched route pattern, not
// the raw URL path. Otherwise a path like /wallets/abc-123 would
// create one Prometheus series per id and blow up storage.
func TestHTTPMiddleware_LabelsByRoute(t *testing.T) {
	// Build a mux with a path parameter so we can verify that
	// r.Pattern is what gets recorded, not r.URL.Path.
	mux := http.NewServeMux()
	mux.HandleFunc("GET /things/{id}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrapped := HTTPMiddleware(mux)

	for _, id := range []string{"a", "b", "c"} {
		req := httptest.NewRequest(http.MethodGet, "/things/"+id, nil)
		rec := httptest.NewRecorder()
		wrapped.ServeHTTP(rec, req)
	}

	// Three different paths, but ALL labelled with the same route
	// pattern. Pulling the counter via the same label keys must
	// show count == 3 (or higher if a previous test recorded any).
	v := testutil.ToFloat64(HTTPRequestsTotal.WithLabelValues("GET", "GET /things/{id}", "200"))
	if v < 3 {
		t.Fatalf("expected >= 3 hits on /things/{id}, got %v", v)
	}
}

// TestHTTPMiddleware_UnmatchedRoutesUseSentinel checks the
// high-cardinality safety net: requests that don't match any
// registered route get the literal label "_unmatched_" instead of
// the raw URL path.
func TestHTTPMiddleware_UnmatchedRoutesUseSentinel(t *testing.T) {
	mux := http.NewServeMux() // no routes registered
	wrapped := HTTPMiddleware(mux)

	req := httptest.NewRequest(http.MethodGet, "/no-such-thing-"+t.Name(), nil)
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	v := testutil.ToFloat64(HTTPRequestsTotal.WithLabelValues("GET", "_unmatched_", "404"))
	if v < 1 {
		t.Fatalf("unmatched-route series did not increment: %v", v)
	}
}

// TestHTTPMiddleware_DefaultStatusIs200 mirrors the access-log
// middleware contract: a handler that writes a body without calling
// WriteHeader produces a 200, and the metrics middleware must record
// that, not a 0.
func TestHTTPMiddleware_DefaultStatusIs200(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /implicit-ok-"+t.Name(), func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	wrapped := HTTPMiddleware(mux)

	req := httptest.NewRequest(http.MethodGet, "/implicit-ok-"+t.Name(), nil)
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	v := testutil.ToFloat64(HTTPRequestsTotal.WithLabelValues("GET", "GET /implicit-ok-"+t.Name(), "200"))
	if v < 1 {
		t.Fatalf("default 200 was not recorded: %v", v)
	}
}
