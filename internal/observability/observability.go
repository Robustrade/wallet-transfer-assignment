package observability

import (
	"io"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

const correlationHeader = "X-Correlation-ID"

type Metrics struct {
	registry           *prometheus.Registry
	requestCount       *prometheus.CounterVec
	requestDuration    *prometheus.HistogramVec
	errorCount         *prometheus.CounterVec
	transferDuration   prometheus.Histogram
	idempotencyHit     prometheus.Counter
	idempotencyPending prometheus.Gauge
}

func NewMetrics(registry *prometheus.Registry) *Metrics {
	if registry == nil {
		registry = prometheus.NewRegistry()
	}

	m := &Metrics{
		registry: registry,
		requestCount: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "wallet_transfer_http_requests_total",
				Help: "Total number of HTTP requests received by the wallet transfer API.",
			},
			[]string{"method", "route", "status"},
		),
		requestDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "wallet_transfer_http_request_duration_seconds",
				Help:    "HTTP request duration distributions for the wallet transfer API.",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"method", "route"},
		),
		errorCount: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "wallet_transfer_errors_total",
				Help: "Total number of errors observed in the wallet transfer API.",
			},
			[]string{"operation"},
		),
		transferDuration: prometheus.NewHistogram(
			prometheus.HistogramOpts{
				Name:    "wallet_transfer_transfer_duration_seconds",
				Help:    "Transfer operation duration distribution.",
				Buckets: prometheus.DefBuckets,
			},
		),
		idempotencyHit: prometheus.NewCounter(
			prometheus.CounterOpts{
				Name: "wallet_transfer_idempotency_hits_total",
				Help: "Total number of idempotency hits for duplicate transfer requests.",
			},
		),
		idempotencyPending: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: "wallet_transfer_idempotency_pending",
				Help: "Current number of PENDING idempotency records (stale claims should be monitored).",
			},
		),
	}

	registry.MustRegister(
		m.requestCount,
		m.requestDuration,
		m.errorCount,
		m.transferDuration,
		m.idempotencyHit,
		m.idempotencyPending,
	)

	return m
}

func NewLogger(writer io.Writer) *slog.Logger {
	if writer == nil {
		writer = os.Stdout
	}
	return slog.New(slog.NewJSONHandler(writer, &slog.HandlerOptions{AddSource: false}))
}

func (m *Metrics) Registry() *prometheus.Registry {
	return m.registry
}

func (m *Metrics) IncRequest(method, route string, status int) {
	if m == nil {
		return
	}
	m.requestCount.WithLabelValues(method, route, http.StatusText(status)).Inc()
}

func (m *Metrics) ObserveRequestDuration(method, route string, seconds float64) {
	if m == nil {
		return
	}
	m.requestDuration.WithLabelValues(method, route).Observe(seconds)
}

func (m *Metrics) IncError(operation string) {
	if m == nil {
		return
	}
	m.errorCount.WithLabelValues(operation).Inc()
}

func (m *Metrics) ObserveTransferDuration(seconds float64) {
	if m == nil {
		return
	}
	m.transferDuration.Observe(seconds)
}

func (m *Metrics) IncIdempotencyHit() {
	if m == nil {
		return
	}
	m.idempotencyHit.Inc()
}

func (m *Metrics) SetIdempotencyPending(n int) {
	if m == nil {
		return
	}
	m.idempotencyPending.Set(float64(n))
}

func HTTPMiddleware(logger *slog.Logger, metrics *Metrics) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rw := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
			next.ServeHTTP(rw, r)
			duration := time.Since(start).Seconds()
			route := r.URL.Path
			if metrics != nil {
				metrics.IncRequest(r.Method, route, rw.statusCode)
				metrics.ObserveRequestDuration(r.Method, route, duration)
				if rw.statusCode >= 400 {
					metrics.IncError("http_request")
				}
			}
			if logger != nil {
				correlationID := rw.Header().Get(correlationHeader)
				if correlationID == "" {
					correlationID = r.Header.Get(correlationHeader)
				}
				logger.Info("http_request",
					slog.String("method", r.Method),
					slog.String("route", route),
					slog.Int("status", rw.statusCode),
					slog.Float64("duration_seconds", duration),
					slog.String("correlation_id", correlationID),
				)
			}
		})
	}
}

type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(statusCode int) {
	rw.statusCode = statusCode
	rw.ResponseWriter.WriteHeader(statusCode)
}
