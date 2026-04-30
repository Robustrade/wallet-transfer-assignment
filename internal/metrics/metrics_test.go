package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

func TestInitRegistersCounters(t *testing.T) {
	Success = prometheus.NewCounter(prometheus.CounterOpts{Name: "transfer_success_total_test"})
	Failure = prometheus.NewCounter(prometheus.CounterOpts{Name: "transfer_failure_total_test"})
	IdempotencyHits = prometheus.NewCounter(prometheus.CounterOpts{Name: "transfer_idempotency_hits_total_test"})

	Init()

	Success.Inc()
	Failure.Inc()
	IdempotencyHits.Inc()
}
