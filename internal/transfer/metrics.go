package transfer

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Metrics struct {
	registry        *prometheus.Registry
	requests        *prometheus.CounterVec
	requestDuration *prometheus.HistogramVec
	transfers       *prometheus.CounterVec
}

func NewMetrics() *Metrics {
	metrics := &Metrics{
		registry: prometheus.NewRegistry(),
		requests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "wallet_transfer",
			Subsystem: "http",
			Name:      "requests_total",
			Help:      "Total number of HTTP requests handled.",
		}, []string{"method", "route", "status"}),
		requestDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "wallet_transfer",
			Subsystem: "http",
			Name:      "request_duration_seconds",
			Help:      "HTTP request latency in seconds.",
			Buckets:   prometheus.DefBuckets,
		}, []string{"method", "route"}),
		transfers: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "wallet_transfer",
			Subsystem: "transfers",
			Name:      "total",
			Help:      "Total number of transfer requests by outcome.",
		}, []string{"outcome"}),
	}
	metrics.registry.MustRegister(metrics.requests, metrics.requestDuration, metrics.transfers)
	return metrics
}

func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}

func (m *Metrics) Middleware(next http.Handler) http.Handler {
	return m.requestMetrics(next)
}

func (m *Metrics) ObserveRequest(method, route string, status int, duration time.Duration) {
	statusCode := strconv.Itoa(status)
	m.requests.WithLabelValues(method, route, statusCode).Inc()
	m.requestDuration.WithLabelValues(method, route).Observe(duration.Seconds())
}

func (m *Metrics) ObserveTransfer(result Transfer, created bool, err error) {
	outcome := "error"
	switch {
	case errors.Is(err, ErrInsufficientFunds):
		outcome = "failed"
	case err == nil && !created:
		outcome = "replayed"
	case err == nil && result.Status == "PROCESSED":
		outcome = "processed"
	case err == nil && result.Status == "FAILED":
		outcome = "failed"
	case err != nil && result.Status == "":
		outcome = "rejected"
	}
	m.transfers.WithLabelValues(outcome).Inc()
}
