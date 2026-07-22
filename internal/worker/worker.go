// Package worker runs the asynchronous transfer pipeline: a pool of claimers
// that drive PENDING transfers to a terminal state, and a reaper that recovers
// transfers abandoned mid-processing.
package worker

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"go.uber.org/zap"
	"wallet-transfer/internal/domain"
	"wallet-transfer/internal/repository"
)

// processor is the subset of service.Processor the worker needs.
type processor interface {
	Process(ctx context.Context, claimed domain.Transfer) error
}

// Config tunes the worker pool and reaper.
type Config struct {
	Count        int
	BatchSize    int
	PollInterval time.Duration
	ReapInterval time.Duration
	StuckAfter   time.Duration
}

// Worker owns the claim/process goroutines and the reaper.
type Worker struct {
	store     repository.Store
	processor processor
	log       *zap.Logger
	cfg       Config
	idPrefix  string
}

func New(store repository.Store, proc processor, log *zap.Logger, cfg Config) *Worker {
	host, _ := os.Hostname()
	return &Worker{
		store:     store,
		processor: proc,
		log:       log,
		cfg:       cfg,
		idPrefix:  fmt.Sprintf("%s-%d", host, os.Getpid()),
	}
}

// Run starts the pool and reaper and blocks until ctx is cancelled, then waits
// for every goroutine to finish (graceful drain).
func (w *Worker) Run(ctx context.Context) error {
	w.log.Info("worker starting",
		zap.Int("count", w.cfg.Count),
		zap.Int("batch_size", w.cfg.BatchSize),
		zap.Duration("poll_interval", w.cfg.PollInterval),
		zap.Duration("reap_interval", w.cfg.ReapInterval))

	var wg sync.WaitGroup
	for i := 0; i < w.cfg.Count; i++ {
		wg.Add(1)
		workerID := fmt.Sprintf("%s-%d", w.idPrefix, i)
		go func() {
			defer wg.Done()
			w.runClaimer(ctx, workerID)
		}()
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		w.runReaper(ctx)
	}()

	wg.Wait()
	w.log.Info("worker stopped")
	return nil
}

// runClaimer repeatedly claims a batch of PENDING transfers and processes them.
// SKIP LOCKED (in the store) means claimers never grab the same row, so you can
// run many of them (even on different machines) without them blocking each other.
func (w *Worker) runClaimer(ctx context.Context, workerID string) {
	for {
		if ctx.Err() != nil {
			return
		}
		claimed, err := w.store.ClaimPending(ctx, workerID, w.cfg.BatchSize)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			w.log.Error("claim failed", zap.String("worker_id", workerID), zap.Error(err))
			w.wait(ctx, w.cfg.PollInterval)
			continue
		}
		if len(claimed) == 0 {
			w.wait(ctx, w.cfg.PollInterval)
			continue
		}
		for _, tr := range claimed {
			// A transient error leaves the transfer PROCESSING for the reaper to
			// recover; we don't stop the claimer for one bad transfer.
			_ = w.processor.Process(ctx, tr)
		}
	}
}

// runReaper regularly puts transfers that are stuck in PROCESSING (because a
// worker stopped mid-way) back to PENDING, so another worker can finish them.
func (w *Worker) runReaper(ctx context.Context) {
	ticker := time.NewTicker(w.cfg.ReapInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			n, err := w.store.ReapStuck(ctx, w.cfg.StuckAfter)
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				w.log.Error("reaper failed", zap.Error(err))
				continue
			}
			if n > 0 {
				w.log.Info("reaper requeued stuck transfers", zap.Int64("count", n))
			}
		}
	}
}

// wait sleeps for d unless ctx is cancelled first.
func (w *Worker) wait(ctx context.Context, d time.Duration) {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}
