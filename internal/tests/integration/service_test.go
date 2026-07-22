package integration_test

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"wallet-transfer/internal/domain"
	"wallet-transfer/internal/repository/postgres"
	"wallet-transfer/internal/service"
	"wallet-transfer/internal/tests/testutils"
)

func enqueueAndClaim(t *testing.T, store *postgres.Store, svc *service.TransferService, from, to uuid.UUID, amount string) domain.Transfer {
	t.Helper()
	ctx := context.Background()
	tr, created, err := svc.CreateTransfer(ctx, domain.NewTransferParams{
		IdempotencyKey: "it-" + uuid.NewString(), FromWalletID: from, ToWalletID: to, Amount: testutils.Money(amount),
	})
	require.NoError(t, err)
	require.True(t, created)
	claimed, err := store.ClaimPending(ctx, "w", 50)
	require.NoError(t, err)
	for _, c := range claimed {
		if c.ID == tr.ID {
			return c
		}
	}
	t.Fatalf("enqueued transfer %s was not claimed", tr.ID)
	return domain.Transfer{}
}

func claimOurs(t *testing.T, store *postgres.Store, want map[uuid.UUID]bool) []domain.Transfer {
	t.Helper()
	ctx := context.Background()
	got := make([]domain.Transfer, 0, len(want))
	for range 20 {
		batch, err := store.ClaimPending(ctx, "w", 50)
		require.NoError(t, err)
		for _, c := range batch {
			if want[c.ID] {
				got = append(got, c)
			}
		}
		if len(got) == len(want) {
			return got
		}
		if len(batch) == 0 {
			break
		}
	}
	t.Fatalf("did not claim all wanted transfers")
	return nil
}

func TestService_CreateTransfer_Idempotent(t *testing.T) {
	pool, store := testutils.NewStore(t)
	svc := service.NewTransferService(store, zap.NewNop(), service.NopRecorder{})
	from := testutils.CreateWallet(t, pool, "100", true)
	to := testutils.CreateWallet(t, pool, "0", true)
	params := domain.NewTransferParams{
		IdempotencyKey: "it-" + uuid.NewString(), FromWalletID: from, ToWalletID: to, Amount: testutils.Money("10"),
	}
	first, created, err := svc.CreateTransfer(context.Background(), params)
	require.NoError(t, err)
	require.True(t, created)
	second, created, err := svc.CreateTransfer(context.Background(), params)
	require.NoError(t, err)
	require.False(t, created)
	require.Equal(t, first.ID, second.ID)
}

func TestService_Process_Happy(t *testing.T) {
	pool, store := testutils.NewStore(t)
	svc := service.NewTransferService(store, zap.NewNop(), service.NopRecorder{})
	proc := service.NewProcessor(store, zap.NewNop(), 5, service.NopRecorder{})
	from := testutils.CreateWallet(t, pool, "100", true)
	to := testutils.CreateWallet(t, pool, "20", true)
	claimed := enqueueAndClaim(t, store, svc, from, to, "30")

	require.NoError(t, proc.Process(context.Background(), claimed))
	require.True(t, testutils.Balance(t, pool, from).Equal(testutils.Money("70")))
	require.True(t, testutils.Balance(t, pool, to).Equal(testutils.Money("50")))
	require.Equal(t, 2, testutils.LedgerCount(t, pool, claimed.ID))

	got, err := store.GetTransferByID(context.Background(), claimed.ID)
	require.NoError(t, err)
	require.Equal(t, domain.StateProcessed, got.State)
}

func TestService_Process_InsufficientFunds_CommitsFailed(t *testing.T) {
	pool, store := testutils.NewStore(t)
	svc := service.NewTransferService(store, zap.NewNop(), service.NopRecorder{})
	proc := service.NewProcessor(store, zap.NewNop(), 5, service.NopRecorder{})
	from := testutils.CreateWallet(t, pool, "50", true)
	to := testutils.CreateWallet(t, pool, "0", true)
	claimed := enqueueAndClaim(t, store, svc, from, to, "80")

	require.NoError(t, proc.Process(context.Background(), claimed))
	require.True(t, testutils.Balance(t, pool, from).Equal(testutils.Money("50")))
	require.Equal(t, 0, testutils.LedgerCount(t, pool, claimed.ID))

	got, err := store.GetTransferByID(context.Background(), claimed.ID)
	require.NoError(t, err)
	require.Equal(t, domain.StateFailed, got.State)
	require.NotNil(t, got.FailureReason)
	require.True(t, strings.Contains(*got.FailureReason, "insufficient"))
}

func TestService_Process_InactiveDestination_CommitsFailed(t *testing.T) {
	pool, store := testutils.NewStore(t)
	svc := service.NewTransferService(store, zap.NewNop(), service.NopRecorder{})
	proc := service.NewProcessor(store, zap.NewNop(), 5, service.NopRecorder{})
	from := testutils.CreateWallet(t, pool, "100", true)
	to := testutils.CreateWallet(t, pool, "0", false)
	claimed := enqueueAndClaim(t, store, svc, from, to, "10")

	require.NoError(t, proc.Process(context.Background(), claimed))
	got, err := store.GetTransferByID(context.Background(), claimed.ID)
	require.NoError(t, err)
	require.Equal(t, domain.StateFailed, got.State)
	require.True(t, testutils.Balance(t, pool, from).Equal(testutils.Money("100")))
}

func TestService_Process_DoubleProcess_IsIdempotent(t *testing.T) {
	pool, store := testutils.NewStore(t)
	svc := service.NewTransferService(store, zap.NewNop(), service.NopRecorder{})
	proc := service.NewProcessor(store, zap.NewNop(), 5, service.NopRecorder{})
	from := testutils.CreateWallet(t, pool, "100", true)
	to := testutils.CreateWallet(t, pool, "0", true)
	claimed := enqueueAndClaim(t, store, svc, from, to, "40")

	require.NoError(t, proc.Process(context.Background(), claimed))
	require.NoError(t, proc.Process(context.Background(), claimed))
	require.True(t, testutils.Balance(t, pool, from).Equal(testutils.Money("60")))
	require.Equal(t, 2, testutils.LedgerCount(t, pool, claimed.ID))
}

func TestService_Process_ConcurrentDebits_NoDoubleSpend(t *testing.T) {
	pool, store := testutils.NewStore(t)
	svc := service.NewTransferService(store, zap.NewNop(), service.NopRecorder{})
	proc := service.NewProcessor(store, zap.NewNop(), 5, service.NopRecorder{})
	ctx := context.Background()
	from := testutils.CreateWallet(t, pool, "100", true)
	d1 := testutils.CreateWallet(t, pool, "0", true)
	d2 := testutils.CreateWallet(t, pool, "0", true)

	ours := make(map[uuid.UUID]bool, 2)
	for _, to := range []uuid.UUID{d1, d2} {
		tr, _, err := svc.CreateTransfer(ctx, domain.NewTransferParams{
			IdempotencyKey: "it-" + uuid.NewString(), FromWalletID: from, ToWalletID: to, Amount: testutils.Money("80"),
		})
		require.NoError(t, err)
		ours[tr.ID] = true
	}
	claimed := claimOurs(t, store, ours)

	var wg sync.WaitGroup
	for _, c := range claimed {
		wg.Add(1)
		go func(tr domain.Transfer) { defer wg.Done(); _ = proc.Process(ctx, tr) }(c)
	}
	wg.Wait()

	var processed, failed int
	for id := range ours {
		got, err := store.GetTransferByID(ctx, id)
		require.NoError(t, err)
		switch got.State {
		case domain.StateProcessed:
			processed++
		case domain.StateFailed:
			failed++
		default:
			t.Fatalf("transfer %s not terminal: %s", id, got.State)
		}
	}
	require.Equal(t, 1, processed)
	require.Equal(t, 1, failed)
	require.True(t, testutils.Balance(t, pool, from).Equal(testutils.Money("20")))
	require.True(t, testutils.Balance(t, pool, d1).Add(testutils.Balance(t, pool, d2)).Equal(testutils.Money("80")))
}
