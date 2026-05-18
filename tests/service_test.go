package service_test

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/candidate/wallet-transfer/internal/config"
	"github.com/candidate/wallet-transfer/internal/db"
	"github.com/candidate/wallet-transfer/internal/domain"
	"github.com/candidate/wallet-transfer/internal/repository"
	"github.com/candidate/wallet-transfer/internal/service"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setup(t *testing.T) (*service.TransferService, *db.DB, *redis.Client, func()) {
	t.Helper()
	os.Setenv("DB_PATH", ":memory:")
	cfg := config.Load()
	database, err := db.New(cfg.DSN())
	require.NoError(t, err)

	schema, err := os.ReadFile("../migrations/001_init.sql")
	require.NoError(t, err)
	_, err = database.Exec(string(schema))
	require.NoError(t, err)

	seed, err := os.ReadFile("../migrations/seed.sql")
	require.NoError(t, err)
	_, err = database.Exec(string(seed))
	require.NoError(t, err)

	redisClient, err := db.NewRedisClient(cfg.RedisAddr)
	if err == nil && redisClient != nil {
		ctx := context.Background()
		_ = redisClient.FlushDB(ctx).Err()
	}

	walletRepo := repository.NewWalletRepository(database)
	txnRepo := repository.NewTransactionRepository(database)
	ledgerRepo := repository.NewLedgerRepository()
	svc := service.NewTransferService(database, redisClient, walletRepo, txnRepo, ledgerRepo)

	cleanup := func() {
		database.Close()
		if redisClient != nil {
			redisClient.Close()
		}
	}
	return svc, database, redisClient, cleanup
}

func createFundedWallet(t *testing.T, svc *service.TransferService, userID uint64, balance string) *domain.Wallet {
	t.Helper()
	wallet, err := svc.CreateWallet(context.Background(), &service.CreateWalletRequest{
		UserID:   userID,
		Currency: "USD",
	})
	require.NoError(t, err)
	return wallet
}

func TestTransfer_Success(t *testing.T) {
	svc, database, _, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()

	req := &service.TransferRequest{
		IdempotencyKey: uuid.NewString(),
		FromWalletID:   1,
		ToWalletID:     2,
		Amount:         decimal.NewFromFloat(100),
		Currency:       "USD",
		Description:    "test transfer",
		Metadata:       `{"client_ip": "127.0.0.1", "device": "iPhone"}`,
	}

	resp, err := svc.Transfer(ctx, req)
	require.NoError(t, err)
	assert.Equal(t, domain.StatusCompleted, resp.Transaction.Status)
	assert.True(t, req.Amount.Equal(resp.Transaction.Amount))
	
	assert.NotEmpty(t, resp.Transaction.ReferenceID)
	assert.True(t, strings.HasPrefix(resp.Transaction.ReferenceID, "TXN_"))
	assert.Equal(t, req.Metadata, resp.Transaction.Metadata)

	var eventType, payload, status string
	err = database.QueryRowContext(ctx, "SELECT event_type, payload, status FROM outbox_events LIMIT 1").Scan(&eventType, &payload, &status)
	require.NoError(t, err)
	assert.Equal(t, "TransferCompleted", eventType)
	assert.Equal(t, "PENDING", status)
	assert.Contains(t, payload, resp.Transaction.ReferenceID)
}

func TestTransfer_Idempotency(t *testing.T) {
	svc, _, _, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()

	key := uuid.NewString()
	req := &service.TransferRequest{
		IdempotencyKey: key,
		FromWalletID:   1,
		ToWalletID:     2,
		Amount:         decimal.NewFromFloat(10),
		Currency:       "USD",
	}

	resp1, err := svc.Transfer(ctx, req)
	require.NoError(t, err)

	resp2, err := svc.Transfer(ctx, req)
	require.NoError(t, err)

	assert.Equal(t, resp1.Transaction.ID, resp2.Transaction.ID)
	assert.True(t, resp1.Transaction.Amount.Equal(resp2.Transaction.Amount))
	assert.Equal(t, resp1.Transaction.ReferenceID, resp2.Transaction.ReferenceID)
}

func TestTransfer_ConcurrentNoDoubleSpend(t *testing.T) {
	svc, database, _, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()

	// Seed Wallet 1 with exactly 50 USD
	_, err := database.Exec("UPDATE wallets SET balance = '50.00000000' WHERE id = 1;")
	require.NoError(t, err)

	const goroutines = 10
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		success int
		fail    int
	)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := svc.Transfer(ctx, &service.TransferRequest{
				IdempotencyKey: uuid.NewString(),
				FromWalletID:   1,
				ToWalletID:     2,
				Amount:         decimal.NewFromFloat(10),
				Currency:       "USD",
			})
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				fail++
			} else {
				success++
			}
		}()
	}
	wg.Wait()

	t.Logf("success=%d fail=%d", success, fail)
	assert.Equal(t, 5, success)
	assert.Equal(t, 5, fail)

	// Verify final balances are perfect
	w1, err := svc.GetWallet(ctx, 1)
	require.NoError(t, err)
	assert.True(t, w1.Balance.Equal(decimal.Zero), "wallet 1 balance must be exactly 0")

	w2, err := svc.GetWallet(ctx, 2)
	require.NoError(t, err)
	assert.True(t, w2.Balance.Equal(decimal.NewFromFloat(550)), "wallet 2 balance must be exactly 550")
}

func TestTransfer_ConcurrentSameIdempotencyKey(t *testing.T) {
	svc, _, _, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()

	key := uuid.NewString()
	const goroutines = 5
	var (
		wg     sync.WaitGroup
		mu     sync.Mutex
		ids    []uint64
		errs   []error
	)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := svc.Transfer(ctx, &service.TransferRequest{
				IdempotencyKey: key,
				FromWalletID:   1,
				ToWalletID:     2,
				Amount:         decimal.NewFromFloat(5),
				Currency:       "USD",
			})
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs = append(errs, err)
			} else {
				ids = append(ids, resp.Transaction.ID)
			}
		}()
	}
	wg.Wait()

	// All goroutines must succeed with zero errors
	require.Empty(t, errs, "all concurrent replays must succeed, got errors: %v", errs)
	require.Len(t, ids, goroutines, "all goroutines must return a transaction ID")
	for _, id := range ids {
		assert.Equal(t, ids[0], id, "all goroutines must return the same transaction ID")
	}
}

func TestTransfer_InsufficientFunds(t *testing.T) {
	svc, _, _, cleanup := setup(t)
	defer cleanup()

	_, err := svc.Transfer(context.Background(), &service.TransferRequest{
		IdempotencyKey: uuid.NewString(),
		FromWalletID:   2,
		ToWalletID:     1,
		Amount:         decimal.NewFromFloat(999999),
		Currency:       "USD",
	})

	assert.ErrorIs(t, err, domain.ErrInsufficientFunds)
}

func TestTransfer_SameWallet(t *testing.T) {
	svc, _, _, cleanup := setup(t)
	defer cleanup()

	_, err := svc.Transfer(context.Background(), &service.TransferRequest{
		IdempotencyKey: uuid.NewString(),
		FromWalletID:   1,
		ToWalletID:     1,
		Amount:         decimal.NewFromFloat(10),
		Currency:       "USD",
	})

	assert.ErrorIs(t, err, domain.ErrSameWallet)
}

func TestTransfer_DoubleEntryLedger(t *testing.T) {
	svc, _, _, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()

	amount := decimal.NewFromFloat(50)
	resp, err := svc.Transfer(ctx, &service.TransferRequest{
		IdempotencyKey: uuid.NewString(),
		FromWalletID:   1,
		ToWalletID:     2,
		Amount:         amount,
		Currency:       "USD",
	})
	require.NoError(t, err)

	fromEntries, err := svc.GetLedger(ctx, 1, 5, 0)
	require.NoError(t, err)
	require.NotEmpty(t, fromEntries)

	latest := fromEntries[0]
	assert.Equal(t, resp.Transaction.ID, latest.TransactionID)
	assert.Equal(t, domain.EntryDebit, latest.EntryType)
	assert.True(t, amount.Equal(latest.Amount))
	assert.True(t, latest.BalanceBefore.Sub(amount).Equal(latest.BalanceAfter))

	toEntries, err := svc.GetLedger(ctx, 2, 5, 0)
	require.NoError(t, err)
	require.NotEmpty(t, toEntries)

	latestTo := toEntries[0]
	assert.Equal(t, domain.EntryCredit, latestTo.EntryType)
	assert.True(t, amount.Equal(latestTo.Amount))
	assert.True(t, latestTo.BalanceBefore.Add(amount).Equal(latestTo.BalanceAfter))
}

func TestTransfer_LedgerCursorPagination(t *testing.T) {
	svc, _, _, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		_, err := svc.Transfer(ctx, &service.TransferRequest{
			IdempotencyKey: uuid.NewString(),
			FromWalletID:   1,
			ToWalletID:     2,
			Amount:         decimal.NewFromFloat(10),
			Currency:       "USD",
		})
		require.NoError(t, err)
	}

	firstPage, err := svc.GetLedger(ctx, 1, 2, 0)
	require.NoError(t, err)
	assert.Len(t, firstPage, 2)
	assert.Greater(t, firstPage[0].ID, firstPage[1].ID)

	cursor := firstPage[1].ID
	secondPage, err := svc.GetLedger(ctx, 1, 2, cursor)
	require.NoError(t, err)
	assert.NotEmpty(t, secondPage)

	for _, entry := range secondPage {
		assert.Less(t, entry.ID, cursor)
	}
}

func TestTransfer_IdempotencyConflict(t *testing.T) {
	svc, _, _, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()

	key := uuid.NewString()
	req1 := &service.TransferRequest{
		IdempotencyKey: key,
		FromWalletID:   1,
		ToWalletID:     2,
		Amount:         decimal.NewFromFloat(10),
		Currency:       "USD",
	}

	_, err := svc.Transfer(ctx, req1)
	require.NoError(t, err)

	// Attempt replay with a different amount
	req2 := &service.TransferRequest{
		IdempotencyKey: key,
		FromWalletID:   1,
		ToWalletID:     2,
		Amount:         decimal.NewFromFloat(20), // mismatched amount!
		Currency:       "USD",
	}

	_, err = svc.Transfer(ctx, req2)
	assert.ErrorIs(t, err, domain.ErrIdempotencyConflict)
}
