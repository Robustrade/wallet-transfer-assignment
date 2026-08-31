// Package metrics provides small, dependency-free Prometheus text metrics.
package metrics

import (
	"fmt"
	"net/http"
	"sync/atomic"
)

// Collector records process-local request and transfer outcome counters.
type Collector struct {
	httpRequests       atomic.Uint64
	processedTransfers atomic.Uint64
	failedTransfers    atomic.Uint64
	replayedTransfers  atomic.Uint64
}

// RecordHTTP increments the counter for completed HTTP requests.
func (c *Collector) RecordHTTP() { c.httpRequests.Add(1) }

// RecordTransfer captures the externally visible result of a transfer request.
func (c *Collector) RecordTransfer(status int, replayed bool) {
	// Stage 1: record the terminal transfer outcome returned to the client.
	if status == http.StatusCreated {
		c.processedTransfers.Add(1)
	}
	if status == http.StatusUnprocessableEntity {
		c.failedTransfers.Add(1)
	}
	// Stage 2: count successful or failed requests served from an idempotent replay.
	if replayed {
		c.replayedTransfers.Add(1)
	}
}

// ServeHTTP emits the current counters in Prometheus' text exposition format.
func (c *Collector) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	_, _ = fmt.Fprintf(w, "# TYPE wallet_http_requests_total counter\nwallet_http_requests_total %d\n", c.httpRequests.Load())
	_, _ = fmt.Fprintf(w, "# TYPE wallet_transfers_processed_total counter\nwallet_transfers_processed_total %d\n", c.processedTransfers.Load())
	_, _ = fmt.Fprintf(w, "# TYPE wallet_transfers_failed_total counter\nwallet_transfers_failed_total %d\n", c.failedTransfers.Load())
	_, _ = fmt.Fprintf(w, "# TYPE wallet_transfers_replayed_total counter\nwallet_transfers_replayed_total %d\n", c.replayedTransfers.Load())
}
