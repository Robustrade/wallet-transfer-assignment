package service

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/AbhishekBadgujar/wallet-transfer-assignment/internals/db"
	"github.com/AbhishekBadgujar/wallet-transfer-assignment/internals/models"
	"github.com/AbhishekBadgujar/wallet-transfer-assignment/internals/service"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

// TestDuplicateTransfer_SameRequestIsReplayed verifies that submitting the
// exact same request twice with the same idempotency key:
//
//   - returns the same transfer ID
//   - returns the same result
//   - moves money only once
//   - creates only one debit and one credit ledger entry
//
// This catches regressions where idempotency is checked but the underlying
// transfer is accidentally executed again.
func TestDuplicateTransfer_SameRequestIsReplayed(t *testing.T) {
	ctx := context.Background()

	dsn := requireTestDSN(t)
	pool, err := db.NewPool(ctx, dsn)
	require.NoError(t, err)
	defer pool.Close()

	fromWallet, toWallet := seedTwoWallets(t, ctx, pool, 10_000)
	svc := buildTransferService(pool)

	input := service.CreateTransferInput{
		IdempotencyKey: uuid.NewString(),
		FromWalletID:   fromWallet,
		ToWalletID:     toWallet,
		Amount:         500,
	}

	// First request actually performs the transfer.
	first, err := svc.CreateTransfer(ctx, input)
	require.NoError(t, err)
	require.NotNil(t, first)
	require.Equal(t, models.TransferProcessed, first.State)

	// Second request is an exact replay.
	second, err := svc.CreateTransfer(ctx, input)
	require.NoError(t, err)
	require.NotNil(t, second)

	// The same transfer must be returned.
	require.Equal(t, first.TransferID, second.TransferID)
	require.Equal(t, first.State, second.State)
	require.Equal(t, first.Amount, second.Amount)
	require.Equal(t, first.FromWalletID, second.FromWalletID)
	require.Equal(t, first.ToWalletID, second.ToWalletID)

	// Money must have moved exactly once.
	assertWalletBalances(t, ctx, pool, fromWallet, toWallet, 9_500, 10_500)

	// Exactly one debit and one credit should exist.
	assertLedgerCountsAndBalance(
		t,
		ctx,
		pool,
		fromWallet,
		toWallet,
		1,
		1,
		500,
		500,
	)
}

// TestDuplicateTransfer_DifferentPayloadReturnsConflict verifies that an
// idempotency key cannot be reused for a different request.
//
// Example:
//
//	request A: key=X, amount=500
//	request B: key=X, amount=700
//
// Request B must fail with ErrIdempotencyKeyConflict and must not move
// additional money.
func TestDuplicateTransfer_DifferentPayloadReturnsConflict(t *testing.T) {
	ctx := context.Background()

	dsn := requireTestDSN(t)
	pool, err := db.NewPool(ctx, dsn)
	require.NoError(t, err)
	defer pool.Close()

	fromWallet, toWallet := seedTwoWallets(t, ctx, pool, 10_000)
	svc := buildTransferService(pool)

	key := uuid.NewString()

	firstInput := service.CreateTransferInput{
		IdempotencyKey: key,
		FromWalletID:   fromWallet,
		ToWalletID:     toWallet,
		Amount:         500,
	}

	first, err := svc.CreateTransfer(ctx, firstInput)
	require.NoError(t, err)
	require.Equal(t, models.TransferProcessed, first.State)

	// Same idempotency key, but different amount.
	secondInput := firstInput
	secondInput.Amount = 700

	second, err := svc.CreateTransfer(ctx, secondInput)

	require.ErrorIs(t, err, service.ErrIdempotencyKeyConflict)
	require.Nil(t, second)

	// The original transfer is the only transfer that should have happened.
	assertWalletBalances(t, ctx, pool, fromWallet, toWallet, 9_500, 10_500)

	assertLedgerCountsAndBalance(
		t,
		ctx,
		pool,
		fromWallet,
		toWallet,
		1,
		1,
		500,
		500,
	)
}

// TestFailedTransfer_InsufficientFunds verifies that an insufficient-funds
// transfer:
//
//   - is marked FAILED
//   - does not modify either wallet
//   - does not create ledger entries
//   - is cached through idempotency
//
// A retry with the same request should replay the same FAILED result rather
// than attempting the transfer again.
func TestFailedTransfer_InsufficientFunds(t *testing.T) {
	ctx := context.Background()

	dsn := requireTestDSN(t)
	pool, err := db.NewPool(ctx, dsn)
	require.NoError(t, err)
	defer pool.Close()

	fromWallet, toWallet := seedTwoWallets(t, ctx, pool, 100)
	svc := buildTransferService(pool)

	input := service.CreateTransferInput{
		IdempotencyKey: uuid.NewString(),
		FromWalletID:   fromWallet,
		ToWalletID:     toWallet,
		Amount:         500,
	}

	first, err := svc.CreateTransfer(ctx, input)
	require.NoError(t, err)
	require.NotNil(t, first)

	require.Equal(t, models.TransferFailed, first.State)
	require.NotEmpty(t, first.FailureReason)

	// Failed transfer must not modify balances.
	assertWalletBalances(t, ctx, pool, fromWallet, toWallet, 100, 100)

	// No money should enter the ledger.
	assertLedgerCountsAndBalance(
		t,
		ctx,
		pool,
		fromWallet,
		toWallet,
		0,
		0,
		0,
		0,
	)

	// Retry the exact same request.
	second, err := svc.CreateTransfer(ctx, input)
	require.NoError(t, err)
	require.NotNil(t, second)

	// Failure should be replayed from idempotency storage.
	require.Equal(t, first.TransferID, second.TransferID)
	require.Equal(t, models.TransferFailed, second.State)
	require.Equal(t, first.FailureReason, second.FailureReason)

	// Still no money movement.
	assertWalletBalances(t, ctx, pool, fromWallet, toWallet, 100, 100)

	assertLedgerCountsAndBalance(
		t,
		ctx,
		pool,
		fromWallet,
		toWallet,
		0,
		0,
		0,
		0,
	)
}

// TestConcurrentTransfers_InsufficientFunds verifies that concurrent
// transfers cannot spend the same wallet balance twice.
//
// Starting balance:
//
//	1000
//
// 10 concurrent requests:
//
//	amount = 150
//
// At most 6 transfers can succeed:
//
//	6 * 150 = 900
//
// The remaining requests must fail with insufficient funds.
//
// The important assertion is that the final balance can never become
// negative and the ledger total must exactly equal the money actually
// transferred.
func TestConcurrentTransfers_InsufficientFunds(t *testing.T) {
	ctx := context.Background()

	dsn := requireTestDSN(t)
	pool, err := db.NewPool(ctx, dsn)
	require.NoError(t, err)
	defer pool.Close()

	fromWallet, toWallet := seedTwoWallets(t, ctx, pool, 1_000)
	svc := buildTransferService(pool)

	const (
		numTransfers = 10
		amountEach   = 150
	)

	var wg sync.WaitGroup

	results := make([]*service.TransferResult, numTransfers)
	errs := make([]error, numTransfers)

	for i := 0; i < numTransfers; i++ {
		wg.Add(1)

		go func(idx int) {
			defer wg.Done()

			results[idx], errs[idx] = svc.CreateTransfer(
				ctx,
				service.CreateTransferInput{
					IdempotencyKey: fmt.Sprintf(
						"insufficient-funds-%s-%d",
						uuid.NewString(),
						idx,
					),
					FromWalletID: fromWallet,
					ToWalletID:   toWallet,
					Amount:       amountEach,
				},
			)
		}(i)
	}

	wg.Wait()

	successCount := 0
	failedCount := 0

	for i := 0; i < numTransfers; i++ {
		require.NoErrorf(t, errs[i], "request %d returned unexpected service error", i)
		require.NotNil(t, results[i])

		switch results[i].State {
		case models.TransferProcessed:
			successCount++

		case models.TransferFailed:
			failedCount++

		default:
			t.Fatalf(
				"request %d returned unexpected state: %s",
				i,
				results[i].State,
			)
		}
	}

	// 1000 / 150 = 6 successful transfers maximum.
	require.Equal(t, 6, successCount)
	require.Equal(t, 4, failedCount)

	// Exactly 900 was transferred.
	assertWalletBalances(
		t,
		ctx,
		pool,
		fromWallet,
		toWallet,
		100,
		1_900,
	)

	// Failed transfers must not create ledger entries.
	assertLedgerCountsAndBalance(
		t,
		ctx,
		pool,
		fromWallet,
		toWallet,
		successCount,
		successCount,
		int64(successCount*amountEach),
		int64(successCount*amountEach),
	)
}

// TestLedgerBalancesForMultipleTransfers explicitly verifies the accounting
// invariant:
//
//	total debits == total credits
//
// This is intentionally independent of the balance assertions so that a
// regression in ledger creation is caught even if wallet balances happen to
// remain correct.
func TestLedgerBalancesForMultipleTransfers(t *testing.T) {
	ctx := context.Background()

	dsn := requireTestDSN(t)
	pool, err := db.NewPool(ctx, dsn)
	require.NoError(t, err)
	defer pool.Close()

	fromWallet, toWallet := seedTwoWallets(t, ctx, pool, 10_000)
	svc := buildTransferService(pool)

	amounts := []int64{100, 250, 375, 500}

	var expectedTotal int64

	for i, amount := range amounts {
		_, err := svc.CreateTransfer(ctx, service.CreateTransferInput{
			IdempotencyKey: fmt.Sprintf(
				"ledger-test-%s-%d",
				uuid.NewString(),
				i,
			),
			FromWalletID: fromWallet,
			ToWalletID:   toWallet,
			Amount:       amount,
		})
		require.NoError(t, err)

		expectedTotal += amount
	}

	assertWalletBalances(
		t,
		ctx,
		pool,
		fromWallet,
		toWallet,
		10_000-expectedTotal,
		10_000+expectedTotal,
	)

	assertLedgerCountsAndBalance(
		t,
		ctx,
		pool,
		fromWallet,
		toWallet,
		len(amounts),
		len(amounts),
		expectedTotal,
		expectedTotal,
	)
}

// assertWalletBalances verifies both sides of the transfer.
func assertWalletBalances(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	fromWallet uuid.UUID,
	toWallet uuid.UUID,
	expectedFrom int64,
	expectedTo int64,
) {
	t.Helper()

	var fromBalance int64
	err := pool.QueryRow(
		ctx,
		`SELECT balance FROM wallets WHERE id = $1`,
		fromWallet,
	).Scan(&fromBalance)
	require.NoError(t, err)

	var toBalance int64
	err = pool.QueryRow(
		ctx,
		`SELECT balance FROM wallets WHERE id = $1`,
		toWallet,
	).Scan(&toBalance)
	require.NoError(t, err)

	require.Equal(t, expectedFrom, fromBalance)
	require.Equal(t, expectedTo, toBalance)
}

// assertLedgerCountsAndBalance verifies both ledger cardinality and the
// accounting invariant:
//
//	debit count == credit count
//	debit sum   == credit sum
func assertLedgerCountsAndBalance(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	fromWallet uuid.UUID,
	toWallet uuid.UUID,
	expectedDebitCount int,
	expectedCreditCount int,
	expectedDebitSum int64,
	expectedCreditSum int64,
) {
	t.Helper()

	var debitCount int
	var debitSum int64

	err := pool.QueryRow(
		ctx,
		`SELECT count(*), COALESCE(sum(amount), 0)
		 FROM ledger_entries
		 WHERE wallet_id = $1
		   AND type = 'DEBIT'`,
		fromWallet,
	).Scan(&debitCount, &debitSum)
	require.NoError(t, err)

	var creditCount int
	var creditSum int64

	err = pool.QueryRow(
		ctx,
		`SELECT count(*), COALESCE(sum(amount), 0)
		 FROM ledger_entries
		 WHERE wallet_id = $1
		   AND type = 'CREDIT'`,
		toWallet,
	).Scan(&creditCount, &creditSum)
	require.NoError(t, err)

	require.Equal(t, expectedDebitCount, debitCount)
	require.Equal(t, expectedCreditCount, creditCount)
	require.Equal(t, expectedDebitSum, debitSum)
	require.Equal(t, expectedCreditSum, creditSum)

	// The fundamental double-entry accounting invariant.
	require.Equal(
		t,
		debitSum,
		creditSum,
		"ledger does not balance",
	)
}
