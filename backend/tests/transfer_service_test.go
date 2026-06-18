package tests

import (
	"context"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"wallet-transfer/database"
	"wallet-transfer/domain"
	"wallet-transfer/repository"
	"wallet-transfer/service"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var db *pgxpool.Pool

var svc *service.TransferService

func TestMain(m *testing.M) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		panic("TEST_DATABASE_URL must be set to run integration tests")
	}

	var err error
	db, err = database.NewPostgresPool(dsn)
	if err != nil {
		panic("failed to connect: " + err.Error())
	}
	defer db.Close()

	walletRepo := repository.NewWalletRepository(db)
	transferRepo := repository.NewTransferRepository(db)
	ledgerRepo := repository.NewLedgerRepository(db)

	svc = service.NewTransferService(db, walletRepo, transferRepo, ledgerRepo)

	os.Exit(m.Run())
}


func createWallet(t *testing.T, balance int64) string {
	t.Helper()
	id := uuid.NewString()
	_, err := db.Exec(context.Background(), `
		INSERT INTO wallets (id, balance) VALUES ($1, $2)
	`, id, balance)
	require.NoError(t, err)

	t.Cleanup(func() {
		db.Exec(context.Background(), `DELETE FROM ledger_entries WHERE wallet_id = $1`, id)
		db.Exec(context.Background(), `DELETE FROM transfers WHERE from_wallet_id = $1 OR to_wallet_id = $1`, id)
		db.Exec(context.Background(), `DELETE FROM wallets WHERE id = $1`, id)
	})

	return id
}

func assertWalletBalance(t *testing.T, walletID string, expected int64) {
	t.Helper()
	var balance int64
	err := db.QueryRow(context.Background(), `
		SELECT balance FROM wallets WHERE id = $1
	`, walletID).Scan(&balance)
	require.NoError(t, err)
	assert.Equal(t, expected, balance)
}

func assertLedgerEntries(t *testing.T, transferID string, expected int) {
	t.Helper()
	var count int
	err := db.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM ledger_entries WHERE transfer_id = $1
	`, transferID).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, expected, count)
}

func transferCountByIdempotencyKey(t *testing.T, key string) int {
	t.Helper()
	var count int
	err := db.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM transfers WHERE idempotency_key = $1
	`, key).Scan(&count)
	require.NoError(t, err)
	return count
}

func getLedgerTotals(t *testing.T, transferID string) (debit, credit int64) {
	t.Helper()
	err := db.QueryRow(context.Background(), `
		SELECT
			COALESCE(SUM(amount) FILTER (WHERE type = 'DEBIT'),  0),
			COALESCE(SUM(amount) FILTER (WHERE type = 'CREDIT'), 0)
		FROM ledger_entries
		WHERE transfer_id = $1
	`, transferID).Scan(&debit, &credit)
	require.NoError(t, err)
	return
}

// ── tests ────────────────────────────────────────────────────────────────────

func TestTransferSuccess(t *testing.T) {
	ctx := context.Background()

	walletA := createWallet(t, 1000)
	walletB := createWallet(t, 500)

	resp, err := svc.CreateTransfer(ctx, service.CreateTransferRequest{
		IdempotencyKey: uuid.NewString(),
		FromWalletID:   walletA,
		ToWalletID:     walletB,
		Amount:         100,
	})

	require.NoError(t, err)
	assert.Equal(t, domain.Processed, resp.Status)

	assertWalletBalance(t, walletA, 900)
	assertWalletBalance(t, walletB, 600)
	assertLedgerEntries(t, resp.ID, 2)
}

func TestTransferInsufficientFunds(t *testing.T) {
	ctx := context.Background()

	walletA := createWallet(t, 50)
	walletB := createWallet(t, 500)

	_, err := svc.CreateTransfer(ctx, service.CreateTransferRequest{
		IdempotencyKey: uuid.NewString(),
		FromWalletID:   walletA,
		ToWalletID:     walletB,
		Amount:         100,
	})

	require.Error(t, err)
	assert.ErrorIs(t, err, service.ErrInsufficientFunds)

	assertWalletBalance(t, walletA, 50)
	assertWalletBalance(t, walletB, 500)
}

func TestTransferIdempotency(t *testing.T) {
	ctx := context.Background()

	walletA := createWallet(t, 1000)
	walletB := createWallet(t, 500)

	key := uuid.NewString()
	req := service.CreateTransferRequest{
		IdempotencyKey: key,
		FromWalletID:   walletA,
		ToWalletID:     walletB,
		Amount:         100,
	}

	first, err := svc.CreateTransfer(ctx, req)
	require.NoError(t, err)

	second, err := svc.CreateTransfer(ctx, req)
	require.NoError(t, err)

	// Same transfer must be returned — no double-spend
	assert.Equal(t, first.ID, second.ID)

	assertWalletBalance(t, walletA, 900)
	assertWalletBalance(t, walletB, 600)
	assertLedgerEntries(t, first.ID, 2)
}

func TestTransferWalletNotFound(t *testing.T) {
	ctx := context.Background()

	walletA := createWallet(t, 1000)

	_, err := svc.CreateTransfer(ctx, service.CreateTransferRequest{
		IdempotencyKey: uuid.NewString(),
		FromWalletID:   walletA,
		ToWalletID:     uuid.NewString(), // non-existent
		Amount:         100,
	})

	require.Error(t, err)
	assert.ErrorIs(t, err, service.ErrWalletNotFound)
}

func TestTransferMissingIdempotencyKey(t *testing.T) {
	ctx := context.Background()

	walletA := createWallet(t, 1000)
	walletB := createWallet(t, 500)

	_, err := svc.CreateTransfer(ctx, service.CreateTransferRequest{
		IdempotencyKey: "", // missing
		FromWalletID:   walletA,
		ToWalletID:     walletB,
		Amount:         100,
	})

	require.Error(t, err)
	assert.ErrorIs(t, err, service.ErrMissingIdempotency)
}

func TestTransferSameWallet(t *testing.T) {
	ctx := context.Background()

	walletA := createWallet(t, 1000)

	_, err := svc.CreateTransfer(ctx, service.CreateTransferRequest{
		IdempotencyKey: uuid.NewString(),
		FromWalletID:   walletA,
		ToWalletID:     walletA,
		Amount:         100,
	})

	require.Error(t, err)
	assert.ErrorIs(t, err, service.ErrSameWallet)
}

func TestTransferInvalidAmount(t *testing.T) {
	ctx := context.Background()

	walletA := createWallet(t, 1000)
	walletB := createWallet(t, 500)

	_, err := svc.CreateTransfer(ctx, service.CreateTransferRequest{
		IdempotencyKey: uuid.NewString(),
		FromWalletID:   walletA,
		ToWalletID:     walletB,
		Amount:         0,
	})

	require.Error(t, err)
	assert.ErrorIs(t, err, service.ErrInvalidAmount)
}

func TestConcurrentTransfers(t *testing.T) {
	ctx := context.Background()

	// walletA has 100, 10 goroutines each try to transfer 20
	// Only 5 should succeed (100 / 20 = 5)
	walletA := createWallet(t, 100)
	walletB := createWallet(t, 0)

	var wg sync.WaitGroup
	successCount := atomic.Int64{}

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := svc.CreateTransfer(ctx, service.CreateTransferRequest{
				IdempotencyKey: fmt.Sprintf("concurrent-key-%d", i),
				FromWalletID:   walletA,
				ToWalletID:     walletB,
				Amount:         20,
			})
			if err == nil {
				successCount.Add(1)
			}
		}(i)
	}

	wg.Wait()

	assert.Equal(t, int64(5), successCount.Load())
	assertWalletBalance(t, walletA, 0)
	assertWalletBalance(t, walletB, 100)
}

func TestConcurrentIdempotency(t *testing.T) {
	ctx := context.Background()

	walletA := createWallet(t, 1000)
	walletB := createWallet(t, 0)
	key := uuid.NewString()

	var wg sync.WaitGroup

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = svc.CreateTransfer(ctx, service.CreateTransferRequest{
				IdempotencyKey: key,
				FromWalletID:   walletA,
				ToWalletID:     walletB,
				Amount:         100,
			})
		}()
	}

	wg.Wait()

	// Only one transfer row must exist for this key
	count := transferCountByIdempotencyKey(t, key)
	assert.Equal(t, 1, count)

	assertWalletBalance(t, walletA, 900)
	assertWalletBalance(t, walletB, 100)
}

func TestLedgerBalanced(t *testing.T) {
	ctx := context.Background()

	walletA := createWallet(t, 1000)
	walletB := createWallet(t, 500)

	transfer, err := svc.CreateTransfer(ctx, service.CreateTransferRequest{
		IdempotencyKey: uuid.NewString(),
		FromWalletID:   walletA,
		ToWalletID:     walletB,
		Amount:         200,
	})

	require.NoError(t, err)

	debitTotal, creditTotal := getLedgerTotals(t, transfer.ID)

	assert.Equal(t, debitTotal, creditTotal)
	assert.Equal(t, int64(200), debitTotal)
}
