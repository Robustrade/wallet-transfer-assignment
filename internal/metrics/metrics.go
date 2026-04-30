package metrics

import "github.com/prometheus/client_golang/prometheus"

var (
	Success = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "transfer_success_total",
		Help: "Total number of successful transfer operations.",
	})
	Failure = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "transfer_failure_total",
		Help: "Total number of failed transfer operations.",
	})
	IdempotencyHits = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "transfer_idempotency_hits_total",
		Help: "Total number of idempotency cache hits.",
	})
)

func Init() {
	prometheus.MustRegister(Success, Failure, IdempotencyHits)
}
