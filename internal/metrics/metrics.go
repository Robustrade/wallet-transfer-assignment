package metrics

import (
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

type Metrics struct {
	requests         *prometheus.CounterVec
	requestDuration  *prometheus.HistogramVec
	transfers        *prometheus.CounterVec
	transferDuration prometheus.Histogram
}

const (
	OutcomeCreated  = "created"
	OutcomeReplayed = "replayed"
	OutcomeRejected = "rejected"
	OutcomeFailed   = "failed"
)

func New(registry prometheus.Registerer) *Metrics {
	factory := promauto.With(registry)

	return &Metrics{
		requests: factory.NewCounterVec(prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "Total HTTP requests by route, method and status.",
		}, []string{"route", "method", "status"}),

		requestDuration: factory.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "http_request_duration_seconds",
			Help:    "HTTP request latency by route.",
			Buckets: prometheus.DefBuckets,
		}, []string{"route", "method"}),

		transfers: factory.NewCounterVec(prometheus.CounterOpts{
			Name: "transfers_total",
			Help: "Transfer attempts by outcome (created, replayed, rejected, failed).",
		}, []string{"outcome"}),

		transferDuration: factory.NewHistogram(prometheus.HistogramOpts{
			Name:    "transfer_duration_seconds",
			Help:    "Time to execute a transfer, including the database transaction.",
			Buckets: prometheus.DefBuckets,
		}),
	}
}

func (m *Metrics) ObserveRequest(route, method string, status int, elapsed time.Duration) {
	if m == nil {
		return
	}
	m.requests.WithLabelValues(route, method, strconv.Itoa(status)).Inc()
	m.requestDuration.WithLabelValues(route, method).Observe(elapsed.Seconds())
}

func (m *Metrics) ObserveTransfer(outcome string, elapsed time.Duration) {
	if m == nil {
		return
	}
	m.transfers.WithLabelValues(outcome).Inc()
	m.transferDuration.Observe(elapsed.Seconds())
}
