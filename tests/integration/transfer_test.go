//go:build integration

package integration

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/synchone/wallet-transfer-assignment/internal/domain"
)

func TestTransferExecution(t *testing.T) {
	env := setupTestEnv(t)
	resetWallets(t, env.pool)
	ctx := context.Background()

	result, err := env.service.CreateTransfer(ctx, domain.CreateTransferInput{
		IdempotencyKey: "transfer-1",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         100,
	})
	require.NoError(t, err)
	assert.Equal(t, domain.StatusProcessed, result.Status)
	assert.False(t, result.IsReplay)
	assert.Len(t, result.LedgerEntryIDs, 2)

	assert.Equal(t, int64(9900), walletBalance(t, env.pool, "wallet_1"))
	assert.Equal(t, int64(5100), walletBalance(t, env.pool, "wallet_2"))
	assert.Equal(t, 1, transferCount(t, env.pool))
	assert.Equal(t, 2, ledgerCount(t, env.pool))
}

func TestIdempotencyReplay(t *testing.T) {
	env := setupTestEnv(t)
	resetWallets(t, env.pool)
	ctx := context.Background()

	input := domain.CreateTransferInput{
		IdempotencyKey: "idem-1",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         250,
	}

	first, err := env.service.CreateTransfer(ctx, input)
	require.NoError(t, err)

	second, err := env.service.CreateTransfer(ctx, input)
	require.NoError(t, err)

	assert.Equal(t, first.TransferID, second.TransferID)
	assert.True(t, second.IsReplay)
	assert.Equal(t, first.LedgerEntryIDs, second.LedgerEntryIDs)
	assert.Equal(t, 1, transferCount(t, env.pool))
	assert.Equal(t, 2, ledgerCount(t, env.pool))
	assert.Equal(t, int64(9750), walletBalance(t, env.pool, "wallet_1"))
}

func TestInsufficientFunds(t *testing.T) {
	env := setupTestEnv(t)
	resetWallets(t, env.pool)
	ctx := context.Background()

	result, err := env.service.CreateTransfer(ctx, domain.CreateTransferInput{
		IdempotencyKey: "fail-1",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         20000,
	})
	require.NoError(t, err)
	assert.Equal(t, domain.StatusFailed, result.Status)
	assert.Equal(t, domain.ErrInsufficientFunds.Error(), result.FailureReason)
	assert.Equal(t, int64(10000), walletBalance(t, env.pool, "wallet_1"))
	assert.Equal(t, 0, ledgerCount(t, env.pool))
}

func TestWalletNotFound(t *testing.T) {
	env := setupTestEnv(t)
	resetWallets(t, env.pool)
	ctx := context.Background()

	_, err := env.service.CreateTransfer(ctx, domain.CreateTransferInput{
		IdempotencyKey: "missing-wallet",
		FromWalletID:   "wallet_missing",
		ToWalletID:     "wallet_2",
		Amount:         100,
	})
	require.Error(t, err)
	assert.True(t, errors.Is(err, domain.ErrWalletNotFound))
	assert.Equal(t, 0, transferCount(t, env.pool))
	assert.Equal(t, 0, ledgerCount(t, env.pool))
}

func TestLedgerDoubleEntry(t *testing.T) {
	env := setupTestEnv(t)
	resetWallets(t, env.pool)
	ctx := context.Background()

	_, err := env.service.CreateTransfer(ctx, domain.CreateTransferInput{
		IdempotencyKey: "ledger-1",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         500,
	})
	require.NoError(t, err)

	var debitSum, creditSum int64
	err = env.pool.QueryRow(ctx, `
		SELECT
			COALESCE(SUM(amount) FILTER (WHERE entry_type = 'DEBIT'), 0),
			COALESCE(SUM(amount) FILTER (WHERE entry_type = 'CREDIT'), 0)
		FROM ledger_entries
	`).Scan(&debitSum, &creditSum)
	require.NoError(t, err)
	assert.Equal(t, int64(500), debitSum)
	assert.Equal(t, debitSum, creditSum)
}

func TestConcurrentDebits(t *testing.T) {
	env := setupTestEnv(t)
	resetWallets(t, env.pool)
	ctx := context.Background()

	_, err := env.pool.Exec(ctx, `UPDATE wallets SET balance = 1000 WHERE id = 'wallet_1'`)
	require.NoError(t, err)

	const attempts = 10
	const amount = int64(200)

	results := make(chan domain.TransferResult, attempts)
	errs := make(chan error, attempts)

	for i := 0; i < attempts; i++ {
		go func(i int) {
			result, err := env.service.CreateTransfer(ctx, domain.CreateTransferInput{
				IdempotencyKey: fmt.Sprintf("concurrent-%d", i),
				FromWalletID:   "wallet_1",
				ToWalletID:     "wallet_2",
				Amount:         amount,
			})
			errs <- err
			if err == nil {
				results <- result
			} else {
				results <- domain.TransferResult{}
			}
		}(i)
	}

	successes := 0
	failures := 0
	for i := 0; i < attempts; i++ {
		require.NoError(t, <-errs)
		result := <-results
		switch result.Status {
		case domain.StatusProcessed:
			successes++
		case domain.StatusFailed:
			failures++
		default:
			t.Fatalf("unexpected status: %s", result.Status)
		}
	}

	assert.Equal(t, 5, successes)
	assert.Equal(t, 5, failures)
	assert.Equal(t, int64(0), walletBalance(t, env.pool, "wallet_1"))
	assert.Equal(t, successes*2, ledgerCount(t, env.pool))
}
