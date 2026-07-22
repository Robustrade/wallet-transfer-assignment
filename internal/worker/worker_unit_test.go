package worker_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"wallet-transfer/internal/domain"
	"wallet-transfer/internal/tests/mock"
	"wallet-transfer/internal/worker"
)

func runWorker(t *testing.T, store *mock.Store, proc *mock.Processor, cfg worker.Config) func() {
	t.Helper()
	w := worker.New(store, proc, zap.NewNop(), cfg)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = w.Run(ctx); close(done) }()
	return func() { cancel(); <-done }
}

func TestWorker_ClaimsProcessesAndReaps(t *testing.T) {
	var claimCalls, reapCalls int32
	proc := &mock.Processor{}
	store := &mock.Store{
		ClaimFn: func(context.Context, string, int) ([]domain.Transfer, error) {
			if atomic.AddInt32(&claimCalls, 1) == 1 {
				return []domain.Transfer{{ID: uuid.New(), State: domain.StateProcessing}}, nil
			}
			return nil, nil // then idle
		},
		ReapFn: func(context.Context, time.Duration) (int64, error) {
			atomic.AddInt32(&reapCalls, 1)
			return 0, nil
		},
	}
	stop := runWorker(t, store, proc, worker.Config{
		Count: 1, BatchSize: 5, PollInterval: 2 * time.Millisecond,
		ReapInterval: 2 * time.Millisecond, StuckAfter: time.Hour,
	})
	defer stop()

	require.Eventually(t, func() bool {
		return proc.Calls() >= 1 && atomic.LoadInt32(&reapCalls) >= 1
	}, 2*time.Second, 5*time.Millisecond)
}

func TestWorker_ProcessorErrorDoesNotStopClaimer(t *testing.T) {
	var calls int32
	proc := &mock.Processor{ProcessFn: func(context.Context, domain.Transfer) error {
		atomic.AddInt32(&calls, 1)
		return errors.New("transient")
	}}
	store := &mock.Store{
		ClaimFn: func(context.Context, string, int) ([]domain.Transfer, error) {
			return []domain.Transfer{{ID: uuid.New(), State: domain.StateProcessing}}, nil
		},
	}
	stop := runWorker(t, store, proc, worker.Config{
		Count: 1, BatchSize: 1, PollInterval: time.Millisecond,
		ReapInterval: time.Hour, StuckAfter: time.Hour,
	})
	defer stop()

	// Claimer keeps going despite processing errors.
	require.Eventually(t, func() bool { return atomic.LoadInt32(&calls) >= 3 }, 2*time.Second, 5*time.Millisecond)
}

func TestWorker_ClaimErrorIsSurvived(t *testing.T) {
	var claimCalls int32
	store := &mock.Store{
		ClaimFn: func(context.Context, string, int) ([]domain.Transfer, error) {
			atomic.AddInt32(&claimCalls, 1)
			return nil, errors.New("db down")
		},
	}
	stop := runWorker(t, store, &mock.Processor{}, worker.Config{
		Count: 1, BatchSize: 1, PollInterval: time.Millisecond,
		ReapInterval: time.Hour, StuckAfter: time.Hour,
	})
	// It retries claiming rather than crashing.
	require.Eventually(t, func() bool { return atomic.LoadInt32(&claimCalls) >= 2 }, 2*time.Second, 5*time.Millisecond)
	stop() // clean shutdown despite persistent claim errors
}

func TestWorker_ReaperErrorIsSurvived(t *testing.T) {
	store := &mock.Store{
		ReapFn: func(context.Context, time.Duration) (int64, error) { return 0, errors.New("reap failed") },
	}
	stop := runWorker(t, store, &mock.Processor{}, worker.Config{
		Count: 1, BatchSize: 1, PollInterval: time.Hour,
		ReapInterval: time.Millisecond, StuckAfter: time.Hour,
	})
	time.Sleep(20 * time.Millisecond) // let the reaper tick and hit the error path
	stop()                            // shuts down cleanly
}
