// Package metrics exposes the project's Prometheus collectors and the
// HTTP middleware that records request metrics.
//
// Design notes:
//
//   - All collectors live as package-level vars (one source of truth)
//     and are registered against a dedicated *prometheus.Registry that
//     this package owns. Using our own registry (rather than the
//     default) keeps tests isolated from each other and prevents
//     "duplicate metrics collector registration attempted" panics
//     when tests run in the same process.
//
//   - The Handler() function returns an http.Handler suitable for
//     mounting at /metrics on the ADMIN listener — never on the
//     public API port. See AGENTS.md §S-16; metrics expose internal
//     state and rates that aid an attacker doing recon.
//
//   - Counters/histograms use the project-prefix `wallet_` so they
//     land cleanly in a multi-service Prometheus instance.
package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Registry is the single Prometheus registry this package uses. It
// is exported so tests can register additional collectors or query
// the gathered values directly via Registry.Gather().
var Registry = prometheus.NewRegistry()

var (
	// TransfersTotal counts terminal transfer outcomes. Label values:
	// "PROCESSED" (successful debit+credit) and "FAILED" (insufficient
	// funds or other committed-failure outcomes).
	TransfersTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "wallet_transfers_total",
			Help: "Total number of completed transfer operations by terminal state.",
		},
		[]string{"state"},
	)

	// TransferDuration observes the wall-clock time taken to process
	// a transfer end-to-end (lock + read + write + commit). The
	// default Prometheus buckets are tuned for sub-second web
	// requests and are appropriate here.
	TransferDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "wallet_transfer_duration_seconds",
			Help:    "End-to-end transfer processing duration in seconds, labelled by terminal state.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"state"},
	)

	// HTTPRequestsTotal counts every HTTP request that completed.
	// Labelled by method, route pattern (NOT raw path — high
	// cardinality kills Prometheus), and status code.
	HTTPRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "wallet_http_requests_total",
			Help: "Total HTTP requests, labelled by method, route pattern, and status code.",
		},
		[]string{"method", "route", "status"},
	)

	// HTTPRequestDuration observes wall-clock latency per request.
	HTTPRequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "wallet_http_request_duration_seconds",
			Help:    "HTTP request duration in seconds, labelled by method and route pattern.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"method", "route"},
	)
)

func init() {
	// Register every collector. Doing it in init() guarantees the
	// metrics are visible from the very first request — there is no
	// "metrics not initialised yet" window during boot.
	Registry.MustRegister(
		TransfersTotal,
		TransferDuration,
		HTTPRequestsTotal,
		HTTPRequestDuration,
		// Also expose the standard process + go runtime collectors.
		// Default Prometheus dashboards rely on these being present.
		// (The `collectors` subpackage is the supported home — the
		// top-level `prometheus.New{Process,Go}Collector` are
		// deprecated since client_golang v1.12.)
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		collectors.NewGoCollector(),
	)
}

// Handler returns the http.Handler that serves the metrics in
// Prometheus exposition format. Mount it at /metrics on the ADMIN
// listener.
func Handler() http.Handler {
	return promhttp.HandlerFor(Registry, promhttp.HandlerOpts{
		// Compress responses when the scraper supports it. Saves
		// bandwidth on busy services.
		EnableOpenMetrics: true,
	})
}
