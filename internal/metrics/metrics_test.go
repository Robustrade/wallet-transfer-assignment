package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestCollectorExposesPrometheusCounters verifies transfer and request outcomes are exported.
func TestCollectorExposesPrometheusCounters(t *testing.T) {
	collector := &Collector{}
	collector.RecordHTTP()
	collector.RecordTransfer(http.StatusCreated, false)
	collector.RecordTransfer(http.StatusUnprocessableEntity, true)
	recorder := httptest.NewRecorder()
	collector.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "wallet_transfers_replayed_total 1") {
		t.Fatalf("unexpected metrics response: %s", recorder.Body.String())
	}
}
