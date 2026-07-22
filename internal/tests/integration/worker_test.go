package integration_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"wallet-transfer/internal/domain"
	"wallet-transfer/internal/repository/postgres"
	"wallet-transfer/internal/service"
	"wallet-transfer/internal/tests/testutils"
	"wallet-transfer/internal/worker"
)

func startWorker(t *testing.T, store *postgres.Store, cfg worker.Config) func() {
	t.Helper()
	proc := service.NewProcessor(store, zap.NewNop(), 5, service.NopRecorder{})
	w := worker.New(store, proc, zap.NewNop(), cfg)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = w.Run(ctx); close(done) }()
	return func() { cancel(); <-done }
}

func stateOf(t *testing.T, store *postgres.Store, id uuid.UUID) domain.TransferState {
	t.Helper()
	tr, err := store.GetTransferByID(context.Background(), id)
	require.NoError(t, err)
	return tr.State
}

func TestWorker_ProcessesEnqueuedTransfer(t *testing.T) {
	pool, store := testutils.NewStore(t)
	from := testutils.CreateWallet(t, pool, "100", true)
	to := testutils.CreateWallet(t, pool, "0", true)
	tr, _, err := store.EnqueueTransfer(context.Background(), domain.NewTransferParams{
		IdempotencyKey: "wk-" + uuid.NewString(), FromWalletID: from, ToWalletID: to, Amount: testutils.Money("40"),
	})
	require.NoError(t, err)

	stop := startWorker(t, store, worker.Config{
		Count: 2, BatchSize: 5, PollInterval: 25 * time.Millisecond,
		ReapInterval: time.Hour, StuckAfter: time.Hour,
	})
	defer stop()

	require.Eventually(t, func() bool {
		return stateOf(t, store, tr.ID) == domain.StateProcessed
	}, 5*time.Second, 25*time.Millisecond)
	require.True(t, testutils.Balance(t, pool, from).Equal(testutils.Money("60")))
	require.True(t, testutils.Balance(t, pool, to).Equal(testutils.Money("40")))
}

func TestWorker_ReaperRecoversAbandonedTransfer(t *testing.T) {
	pool, store := testutils.NewStore(t)
	from := testutils.CreateWallet(t, pool, "100", true)
	to := testutils.CreateWallet(t, pool, "0", true)
	tr, _, err := store.EnqueueTransfer(context.Background(), domain.NewTransferParams{
		IdempotencyKey: "wk-" + uuid.NewString(), FromWalletID: from, ToWalletID: to, Amount: testutils.Money("25"),
	})
	require.NoError(t, err)
	_, err = pool.Exec(context.Background(),
		`UPDATE transfer SET state='PROCESSING', claimed_at = now() - interval '1 hour', worker_id='dead' WHERE id=$1`, tr.ID)
	require.NoError(t, err)

	stop := startWorker(t, store, worker.Config{
		Count: 1, BatchSize: 5, PollInterval: 25 * time.Millisecond,
		ReapInterval: 50 * time.Millisecond, StuckAfter: 50 * time.Millisecond,
	})
	defer stop()

	require.Eventually(t, func() bool {
		return stateOf(t, store, tr.ID) == domain.StateProcessed
	}, 5*time.Second, 25*time.Millisecond)
	require.True(t, testutils.Balance(t, pool, from).Equal(testutils.Money("75")))
}
