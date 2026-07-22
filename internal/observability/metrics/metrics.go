// Package metrics owns the Prometheus registry and application collectors.
package metrics

import (
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics holds all collectors on a private registry (no global state).
type Metrics struct {
	reg *prometheus.Registry

	httpRequests       *prometheus.CounterVec
	httpDuration       *prometheus.HistogramVec
	transfersEnqueued  prometheus.Counter
	transfersProcessed *prometheus.CounterVec
	processingDuration *prometheus.HistogramVec
}

func New() *Metrics {
	reg := prometheus.NewRegistry()
	m := &Metrics{
		reg: reg,
		httpRequests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "http_requests_total", Help: "Total HTTP requests by method, route, and status.",
		}, []string{"method", "route", "status"}),
		httpDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name: "http_request_duration_seconds", Help: "HTTP request latency.", Buckets: prometheus.DefBuckets,
		}, []string{"method", "route"}),
		transfersEnqueued: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "transfers_enqueued_total", Help: "Transfers accepted and enqueued.",
		}),
		transfersProcessed: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "transfers_processed_total", Help: "Transfers reaching a terminal state by result.",
		}, []string{"result"}),
		processingDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name: "transfer_processing_duration_seconds", Help: "Worker processing latency by result.", Buckets: prometheus.DefBuckets,
		}, []string{"result"}),
	}
	reg.MustRegister(
		m.httpRequests, m.httpDuration,
		m.transfersEnqueued, m.transfersProcessed, m.processingDuration,
		collectors.NewGoCollector(),
	)
	return m
}

// Handler serves the private registry at /metrics.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.reg, promhttp.HandlerOpts{})
}

// ObserveHTTP records one HTTP request.
func (m *Metrics) ObserveHTTP(method, route string, status int, d time.Duration) {
	m.httpRequests.WithLabelValues(method, route, strconv.Itoa(status)).Inc()
	m.httpDuration.WithLabelValues(method, route).Observe(d.Seconds())
}

// TransferEnqueued / TransferProcessed satisfy service.Recorder.
func (m *Metrics) TransferEnqueued() { m.transfersEnqueued.Inc() }

func (m *Metrics) TransferProcessed(result string, d time.Duration) {
	m.transfersProcessed.WithLabelValues(result).Inc()
	m.processingDuration.WithLabelValues(result).Observe(d.Seconds())
}

// RegisterDBPool reports how busy the database connection pool is, which is an
// early warning that the pool is running out of connections under load.
func (m *Metrics) RegisterDBPool(pool *pgxpool.Pool) {
	m.reg.MustRegister(
		prometheus.NewGaugeFunc(prometheus.GaugeOpts{
			Name: "db_pool_acquired_conns", Help: "Connections currently in use.",
		}, func() float64 { return float64(pool.Stat().AcquiredConns()) }),
		prometheus.NewGaugeFunc(prometheus.GaugeOpts{
			Name: "db_pool_idle_conns", Help: "Idle connections in the pool.",
		}, func() float64 { return float64(pool.Stat().IdleConns()) }),
		prometheus.NewGaugeFunc(prometheus.GaugeOpts{
			Name: "db_pool_total_conns", Help: "Total open connections.",
		}, func() float64 { return float64(pool.Stat().TotalConns()) }),
		prometheus.NewGaugeFunc(prometheus.GaugeOpts{
			Name: "db_pool_max_conns", Help: "Max configured connections.",
		}, func() float64 { return float64(pool.Stat().MaxConns()) }),
	)
}
