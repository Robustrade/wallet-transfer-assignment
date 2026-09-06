package observability

import (
	"sync/atomic"
)

type Metrics struct {
	TotalRequests          uint64
	SuccessfulTransfers    uint64
	FailedTransfers        uint64
	InsufficientFundsCount uint64
}

var DefaultMetrics = &Metrics{}

func (m *Metrics) IncTotalRequests() {
	atomic.AddUint64(&m.TotalRequests, 1)
}

func (m *Metrics) IncSuccessfulTransfers() {
	atomic.AddUint64(&m.SuccessfulTransfers, 1)
}

func (m *Metrics) IncFailedTransfers() {
	atomic.AddUint64(&m.FailedTransfers, 1)
}

func (m *Metrics) IncInsufficientFunds() {
	atomic.AddUint64(&m.InsufficientFundsCount, 1)
}
