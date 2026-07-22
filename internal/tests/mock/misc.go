package mock

import (
	"context"
	"sync"
	"time"

	"wallet-transfer/internal/domain"
)

// Recorder is a fake service.Recorder that counts calls (thread-safe).
type Recorder struct {
	mu        sync.Mutex
	Enqueued  int
	Processed []string
}

func (r *Recorder) TransferEnqueued() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Enqueued++
}

func (r *Recorder) TransferProcessed(result string, _ time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Processed = append(r.Processed, result)
}

// ProcessedCount returns how many times a given result was recorded.
func (r *Recorder) ProcessedCount(result string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, x := range r.Processed {
		if x == result {
			n++
		}
	}
	return n
}

// Processor is a fake worker processor that records calls.
type Processor struct {
	mu        sync.Mutex
	ProcessFn func(context.Context, domain.Transfer) error
	calls     int
}

func (p *Processor) Process(ctx context.Context, tr domain.Transfer) error {
	p.mu.Lock()
	p.calls++
	p.mu.Unlock()
	if p.ProcessFn != nil {
		return p.ProcessFn(ctx, tr)
	}
	return nil
}

func (p *Processor) Calls() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}
