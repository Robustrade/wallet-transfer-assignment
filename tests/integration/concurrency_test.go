package integration

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConcurrency_SimultaneousDebitsFromSameWallet(t *testing.T) {
	app := SetupTestApp(t)
	SeedWallets(t, app)

	// wallet_1 has 100000 ($1000)
	// Send 20 concurrent transfers of 5000 ($50) each = 100000 total
	// This should allow all to succeed since total == balance

	numTransfers := 20
	transferAmount := int64(5000)

	var wg sync.WaitGroup
	var successCount atomic.Int32
	var failCount atomic.Int32

	for i := 0; i < numTransfers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			_, err := app.TransferService.CreateTransfer(
				context.Background(),
				fmt.Sprintf("concurrent-debit-%d", idx),
				"wallet_1",
				"wallet_2",
				transferAmount,
			)

			if err != nil {
				failCount.Add(1)
			} else {
				successCount.Add(1)
			}
		}(i)
	}

	wg.Wait()

	finalBalance := GetWalletBalance(t, app, "wallet_1")
	successTotal := int64(successCount.Load()) * transferAmount

	// Verify: initialBalance - (successful * amount) == final balance
	assert.Equal(t, int64(100000)-successTotal, finalBalance,
		"final balance must equal initial minus total debited")

	// Verify no negative balance
	assert.GreaterOrEqual(t, finalBalance, int64(0),
		"wallet balance must never go negative")

	// All 20 should succeed since 20*5000 = 100000 exactly
	assert.Equal(t, int32(20), successCount.Load(), "all 20 transfers should succeed")
	assert.Equal(t, int32(0), failCount.Load(), "no transfers should fail")

	// wallet_2 should have received all successful amounts
	wallet2Balance := GetWalletBalance(t, app, "wallet_2")
	assert.Equal(t, int64(50000)+successTotal, wallet2Balance)

	t.Logf("Results: %d succeeded, %d failed, final balance: %d",
		successCount.Load(), failCount.Load(), finalBalance)
}

func TestConcurrency_OverdraftPrevention(t *testing.T) {
	app := SetupTestApp(t)
	SeedWallets(t, app)

	// wallet_3 has 25000 ($250)
	// Send 10 concurrent transfers of 5000 ($50) each = 50000 total
	// Only 5 should succeed; the rest should fail with insufficient funds

	numTransfers := 10
	transferAmount := int64(5000)

	var wg sync.WaitGroup
	var successCount atomic.Int32
	var failCount atomic.Int32

	for i := 0; i < numTransfers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			_, err := app.TransferService.CreateTransfer(
				context.Background(),
				fmt.Sprintf("overdraft-test-%d", idx),
				"wallet_3",
				"wallet_1",
				transferAmount,
			)

			if err != nil {
				failCount.Add(1)
			} else {
				successCount.Add(1)
			}
		}(i)
	}

	wg.Wait()

	finalBalance := GetWalletBalance(t, app, "wallet_3")

	// Verify no double spending
	assert.GreaterOrEqual(t, finalBalance, int64(0),
		"wallet balance must never go negative (no double spend)")

	// Exactly 5 should succeed: 25000 / 5000 = 5
	assert.Equal(t, int32(5), successCount.Load(),
		"exactly 5 transfers should succeed (25000 / 5000)")
	assert.Equal(t, int32(5), failCount.Load(),
		"exactly 5 transfers should fail")

	assert.Equal(t, int64(0), finalBalance,
		"wallet_3 should be fully drained to 0")

	// Verify ledger consistency
	var totalDebits, totalCredits int64
	err := app.Pool.QueryRow(context.Background(),
		`SELECT COALESCE(SUM(amount), 0) FROM ledger_entries WHERE entry_type = 'DEBIT'`,
	).Scan(&totalDebits)
	require.NoError(t, err)

	err = app.Pool.QueryRow(context.Background(),
		`SELECT COALESCE(SUM(amount), 0) FROM ledger_entries WHERE entry_type = 'CREDIT'`,
	).Scan(&totalCredits)
	require.NoError(t, err)

	assert.Equal(t, totalDebits, totalCredits, "ledger must balance under concurrency")

	t.Logf("Results: %d succeeded, %d failed, final balance: %d",
		successCount.Load(), failCount.Load(), finalBalance)
}

func TestConcurrency_CrossWalletTransfers(t *testing.T) {
	app := SetupTestApp(t)
	SeedWallets(t, app)

	// Simulate bidirectional transfers between wallet_1 and wallet_2
	// This tests deadlock prevention (deterministic lock ordering)

	numTransfers := 10
	transferAmount := int64(1000)

	var wg sync.WaitGroup
	var successCount atomic.Int32

	for i := 0; i < numTransfers; i++ {
		wg.Add(2)

		// wallet_1 -> wallet_2
		go func(idx int) {
			defer wg.Done()
			_, err := app.TransferService.CreateTransfer(
				context.Background(),
				fmt.Sprintf("cross-1to2-%d", idx),
				"wallet_1",
				"wallet_2",
				transferAmount,
			)
			if err == nil {
				successCount.Add(1)
			}
		}(i)

		// wallet_2 -> wallet_1
		go func(idx int) {
			defer wg.Done()
			_, err := app.TransferService.CreateTransfer(
				context.Background(),
				fmt.Sprintf("cross-2to1-%d", idx),
				"wallet_2",
				"wallet_1",
				transferAmount,
			)
			if err == nil {
				successCount.Add(1)
			}
		}(i)
	}

	wg.Wait()

	// No deadlocks should occur — all transfers should complete
	// (either succeed or fail gracefully with insufficient funds)
	t.Logf("Cross-wallet transfers: %d succeeded out of %d", successCount.Load(), numTransfers*2)

	// Verify balances are non-negative
	balance1 := GetWalletBalance(t, app, "wallet_1")
	balance2 := GetWalletBalance(t, app, "wallet_2")

	assert.GreaterOrEqual(t, balance1, int64(0), "wallet_1 must not go negative")
	assert.GreaterOrEqual(t, balance2, int64(0), "wallet_2 must not go negative")

	// Verify total balance is conserved
	totalBefore := int64(100000 + 50000 + 25000)
	totalAfter := balance1 + balance2 + GetWalletBalance(t, app, "wallet_3")
	assert.Equal(t, totalBefore, totalAfter, "total balance must be conserved")

	// Verify ledger consistency
	var totalDebits, totalCredits int64
	err := app.Pool.QueryRow(context.Background(),
		`SELECT COALESCE(SUM(amount), 0) FROM ledger_entries WHERE entry_type = 'DEBIT'`,
	).Scan(&totalDebits)
	require.NoError(t, err)

	err = app.Pool.QueryRow(context.Background(),
		`SELECT COALESCE(SUM(amount), 0) FROM ledger_entries WHERE entry_type = 'CREDIT'`,
	).Scan(&totalCredits)
	require.NoError(t, err)

	assert.Equal(t, totalDebits, totalCredits, "ledger must balance")
}

func TestConcurrency_SameIdempotencyKeyConcurrent(t *testing.T) {
	app := SetupTestApp(t)
	SeedWallets(t, app)

	// Send 20 concurrent requests with the SAME idempotency key.
	// Only one should actually process; the rest should be idempotent replays.

	numWorkers := 20
	var wg sync.WaitGroup
	var successCount atomic.Int32
	var replayCount atomic.Int32

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			result, err := app.TransferService.CreateTransfer(
				context.Background(),
				"same-key-concurrent",
				"wallet_1",
				"wallet_2",
				1000,
			)

			if err == nil {
				if result.IsDuplicate {
					replayCount.Add(1)
				} else {
					successCount.Add(1)
				}
			}
		}()
	}

	wg.Wait()

	// Exactly 1 should be the original, rest are replays
	assert.Equal(t, int32(1), successCount.Load(),
		"exactly 1 transfer should be the original")
	assert.Equal(t, int32(numWorkers-1), replayCount.Load(),
		"remaining should be idempotent replays")

	// Balance should reflect exactly 1 transfer
	assert.Equal(t, int64(99000), GetWalletBalance(t, app, "wallet_1"))
	assert.Equal(t, int64(51000), GetWalletBalance(t, app, "wallet_2"))
}
