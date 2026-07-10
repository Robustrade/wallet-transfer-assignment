package postgres_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/loopinnovates/wallet-transfer-assignment/internal/domain"
	"github.com/loopinnovates/wallet-transfer-assignment/repository/postgres"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A context that's already past its deadline when ExecuteTransfer is
// called simulates the timeout case discussed for wallet.svc.go's
// transferTimeout: the very first DB operation (locking the wallets in
// insertPendingTransfer's own transaction) must fail fast with
// context.DeadlineExceeded, must not be retried by withRetry (context
// errors are explicitly excluded from isRetryableError), and must leave no
// transfer row or balance change behind.
func TestExecuteTransfer_ContextTimeout_LeavesNoTraceAndDoesNotRetry(t *testing.T) {
	db := testDB(t)
	repo := postgres.NewTransferRepository(db, db)

	fromWallet := createTestWallet(t, db, 1000)
	toWallet := createTestWallet(t, db, 0)
	idempotencyKey := fmt.Sprintf("context-timeout-%s", uniqueRunToken())

	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	// Make sure the deadline has actually passed before we even call in -
	// a 1ns timeout can otherwise race against how fast the goroutine
	// scheduler gets here.
	<-ctx.Done()

	start := time.Now()
	_, err := repo.ExecuteTransfer(ctx, idempotencyKey, fromWallet, toWallet, 100, 1)
	elapsed := time.Since(start)

	assert.ErrorIs(t, err, context.DeadlineExceeded)
	// withRetry's backoff schedule is 50ms/100ms/200ms between attempts;
	// if a context error were (incorrectly) retried this would take at
	// least that long. A near-instant failure is evidence it wasn't.
	assert.Less(t, elapsed, 40*time.Millisecond, "context.DeadlineExceeded should fail immediately, not go through withRetry's backoff")

	persisted, getErr := repo.GetByIdempotencyKey(context.Background(), idempotencyKey)
	require.NoError(t, getErr)
	assert.Nil(t, persisted, "a request that never got a chance to run must not leave a transfer row")

	assert.Equal(t, 1000.0, getWalletBalance(t, db, fromWallet))
	assert.Equal(t, 0.0, getWalletBalance(t, db, toWallet))
}

// Many distinct sender wallets paying into a single receiver - e.g. a
// checkout/payout flow - hammers one specific wallet row with FOR UPDATE
// contention from every concurrent transfer, unlike
// TestExecuteTransfer_ConcurrentTransfers_NoLostUpdates (same fixed pair
// every time) or TestExecuteTransfer_FiveUsersConcurrentTransfers (random
// pairs, no guaranteed shared wallet). This is the pattern most likely to
// expose a lock-ordering or lost-update bug specific to one row being the
// target of everyone's transaction at once.
func TestExecuteTransfer_ManyToOneHotspot_NoLostUpdates(t *testing.T) {
	db := testDB(t)
	repo := postgres.NewTransferRepository(db, db)
	ctx := context.Background()

	const (
		numCustomers    = 20
		customerBalance = 1_000.0
		transferAmount  = 50.0
	)

	merchant := createTestWallet(t, db, 0)
	customers := make([]string, numCustomers)
	for i := range customers {
		customers[i] = createTestWallet(t, db, customerBalance)
	}

	runToken := uniqueRunToken()
	var wg sync.WaitGroup
	errs := make([]error, numCustomers)

	for i, customer := range customers {
		wg.Add(1)
		go func(i int, customer string) {
			defer wg.Done()
			_, err := repo.ExecuteTransfer(ctx, fmt.Sprintf("hotspot-%s-%d", runToken, i), customer, merchant, transferAmount, int64(i))
			errs[i] = err
		}(i, customer)
	}
	wg.Wait()

	for i, err := range errs {
		assert.NoError(t, err, "payment %d into the hotspot wallet should not have failed", i)
	}

	assert.Equal(t, numCustomers*transferAmount, getWalletBalance(t, db, merchant), "hotspot wallet must reflect every single incoming payment, not lose any to a race")
	for i, customer := range customers {
		assert.Equal(t, customerBalance-transferAmount, getWalletBalance(t, db, customer), "customer %d balance mismatch", i)
	}
}

// Boundary conditions on the balance check in moveFunds: a transfer for
// exactly the wallet's current balance must succeed and leave it at
// exactly zero (the check is `fromBalance < amount`, not <=), while a
// transfer for one cent more than the balance must fail and be recorded as
// FAILED, leaving the balance untouched.
func TestExecuteTransfer_ExactBalanceBoundary(t *testing.T) {
	db := testDB(t)
	repo := postgres.NewTransferRepository(db, db)
	ctx := context.Background()

	t.Run("amount equal to balance succeeds and leaves zero", func(t *testing.T) {
		const balance = 250.0
		fromWallet := createTestWallet(t, db, balance)
		toWallet := createTestWallet(t, db, 0)

		transfer, err := repo.ExecuteTransfer(ctx, fmt.Sprintf("exact-balance-%s", uniqueRunToken()), fromWallet, toWallet, balance, 1)
		require.NoError(t, err)
		require.NotNil(t, transfer)
		assert.Equal(t, domain.TransferProcessed, transfer.Status)

		assert.Equal(t, 0.0, getWalletBalance(t, db, fromWallet))
		assert.Equal(t, balance, getWalletBalance(t, db, toWallet))
	})

	t.Run("amount one cent over balance fails and is recorded", func(t *testing.T) {
		const balance = 250.0
		const amount = balance + 0.01
		fromWallet := createTestWallet(t, db, balance)
		toWallet := createTestWallet(t, db, 0)

		transfer, err := repo.ExecuteTransfer(ctx, fmt.Sprintf("exact-balance-over-%s", uniqueRunToken()), fromWallet, toWallet, amount, 1)
		require.NoError(t, err, "insufficient balance is a recorded outcome, not a Go error")
		require.NotNil(t, transfer)
		assert.Equal(t, domain.TransferFailed, transfer.Status)

		assert.Equal(t, balance, getWalletBalance(t, db, fromWallet))
		assert.Equal(t, 0.0, getWalletBalance(t, db, toWallet))
	})
}
