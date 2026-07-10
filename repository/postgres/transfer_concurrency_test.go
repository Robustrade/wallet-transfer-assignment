package postgres_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/lib/pq"
	"github.com/loopinnovates/wallet-transfer-assignment/internal/config"
	"github.com/loopinnovates/wallet-transfer-assignment/internal/domain"
	"github.com/loopinnovates/wallet-transfer-assignment/internal/service"
	"github.com/loopinnovates/wallet-transfer-assignment/repository/postgres"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testDB(t *testing.T) *sql.DB {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping Postgres concurrency test in -short mode")
	}

	appConfig := config.AppConfig()

	dsn := appConfig.ReadPGURI

	// Bound the pool: these tests fire far more concurrent goroutines than
	// Postgres's max_connections, and each ExecuteTransfer now opens two
	// sequential connections (phase 1 + phase 2). Without a cap,
	// database/sql happily opens one connection per goroutine and Postgres
	// starts rejecting them with "too many clients already" - a pool
	// exhaustion error, not a correctness bug.
	db, err := postgres.NewDB(dsn, postgres.PoolConfig{MaxOpenConns: 50, MaxIdleConns: 50})
	if err != nil {
		t.Skipf("skipping: no reachable Postgres at %s (%v) - run `docker compose up -d`", dsn, err)
	}

	t.Cleanup(func() { _ = db.Close() })
	return db
}

func createTestWallet(t *testing.T, db *sql.DB, balance float64) string {
	t.Helper()

	var id string
	err := db.QueryRow(
		`INSERT INTO wallets (owner_name, balance) VALUES ($1, $2) RETURNING id`,
		fmt.Sprintf("test-wallet-%s", t.Name()), balance,
	).Scan(&id)
	require.NoError(t, err)

	t.Cleanup(func() {
		if _, err := db.Exec(`DELETE FROM ledger_entries WHERE transfer_id IN (SELECT id FROM transfers WHERE from_wallet_id = $1 OR to_wallet_id = $1)`, id); err != nil {
			t.Logf("cleanup: delete ledger_entries for wallet %s: %v", id, err)
		}
		if _, err := db.Exec(`DELETE FROM transfers WHERE from_wallet_id = $1 OR to_wallet_id = $1`, id); err != nil {
			t.Logf("cleanup: delete transfers for wallet %s: %v", id, err)
		}
		if _, err := db.Exec(`DELETE FROM wallets WHERE id = $1`, id); err != nil {
			t.Logf("cleanup: delete wallet %s: %v", id, err)
		}
	})
	return id
}

func uniqueRunToken() string {
	return fmt.Sprintf("%d-%d", time.Now().UnixNano(), rand.Int63())
}

func getWalletBalance(t *testing.T, db *sql.DB, walletID string) float64 {
	t.Helper()
	var balance float64
	require.NoError(t, db.QueryRow(`SELECT balance FROM wallets WHERE id = $1`, walletID).Scan(&balance))
	return balance
}

func TestExecuteTransfer_ConcurrentTransfers_NoLostUpdates(t *testing.T) {
	db := testDB(t)
	repo := postgres.NewTransferRepository(db, db)
	ctx := context.Background()

	const (
		numTransfers   = 50
		transferAmount = 10.0
		startBalance   = 100_000.0
	)

	fromWallet := createTestWallet(t, db, startBalance)
	toWallet := createTestWallet(t, db, 0)

	var wg sync.WaitGroup
	errs := make([]error, numTransfers)

	for i := 0; i < numTransfers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := repo.ExecuteTransfer(ctx, fmt.Sprintf("concurrent-lost-update-%s-%d", t.Name(), i), fromWallet, toWallet, transferAmount, int64(i))
			errs[i] = err
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		assert.NoError(t, err, "transfer %d should not have failed", i)
	}

	assert.Equal(t, startBalance-numTransfers*transferAmount, getWalletBalance(t, db, fromWallet))
	assert.Equal(t, numTransfers*transferAmount, getWalletBalance(t, db, toWallet))
}

func TestExecuteTransfer_ConcurrentReverseTransfers_NoDeadlock(t *testing.T) {
	db := testDB(t)
	repo := postgres.NewTransferRepository(db, db)
	ctx := context.Background()

	const (
		numEachDirection = 40
		transferAmount   = 5.0
		startBalance     = 10_000.0
	)

	walletA := createTestWallet(t, db, startBalance)
	walletB := createTestWallet(t, db, startBalance)

	var wg sync.WaitGroup
	errs := make([]error, numEachDirection*2)

	run := func(idx int, from, to string) {
		defer wg.Done()
		_, err := repo.ExecuteTransfer(ctx, fmt.Sprintf("concurrent-reverse-%s-%d", t.Name(), idx), from, to, transferAmount, int64(idx))
		errs[idx] = err
	}

	for i := 0; i < numEachDirection; i++ {
		wg.Add(2)
		go run(i*2, walletA, walletB)
		go run(i*2+1, walletB, walletA)
	}
	wg.Wait()

	for i, err := range errs {
		if err == nil {
			continue
		}
		var pqErr *pq.Error
		if errors.As(err, &pqErr) {
			require.NotEqual(t, "40P01", string(pqErr.Code), "transfer %d deadlocked: sorted wallet locking is broken", i)
		}
		t.Errorf("transfer %d should not have failed: %v", i, err)
	}

	assert.Equal(t, startBalance, getWalletBalance(t, db, walletA))
	assert.Equal(t, startBalance, getWalletBalance(t, db, walletB))
}

// TestExecuteTransfer_ConcurrentReverseTransfers_NoDeadlock only exercises a
// two-wallet lock cycle. Real usage has many users transferring to each
// other simultaneously, which stresses lockWalletPair's sorted-order
// locking across a much larger, randomly-shaped lock graph than a single
// A<->B pair - this is the more general case sorted locking has to hold up
// under, not just the pairwise one. 5 wallets fire 200 transfers at random
// (non-self) pairs concurrently; we assert no deadlocks, every transfer
// lands, and each wallet's final balance matches the exact sum of the
// transfers that touched it - not just that the grand total is conserved.
func TestExecuteTransfer_FiveUsersConcurrentTransfers(t *testing.T) {
	db := testDB(t)
	repo := postgres.NewTransferRepository(db, db)
	ctx := context.Background()

	const (
		numUsers       = 5
		numTransfers   = 200
		transferAmount = 10.0
		startBalance   = 100_000.0
	)

	wallets := make([]string, numUsers)
	for i := range wallets {
		wallets[i] = createTestWallet(t, db, startBalance)
	}

	type pair struct{ from, to int }
	attempts := make([]pair, numTransfers)
	for i := range attempts {
		from := rand.Intn(numUsers)
		to := rand.Intn(numUsers)
		for to == from {
			to = rand.Intn(numUsers)
		}
		attempts[i] = pair{from, to}
	}

	runToken := uniqueRunToken()
	var wg sync.WaitGroup
	errs := make([]error, numTransfers)

	for i, a := range attempts {
		wg.Add(1)
		go func(i int, a pair) {
			defer wg.Done()
			_, err := repo.ExecuteTransfer(ctx, fmt.Sprintf("five-users-%s-%d", runToken, i), wallets[a.from], wallets[a.to], transferAmount, int64(i))
			errs[i] = err
		}(i, a)
	}
	wg.Wait()

	deltas := make([]float64, numUsers)
	for i, err := range errs {
		if err != nil {
			var pqErr *pq.Error
			if errors.As(err, &pqErr) {
				require.NotEqual(t, "40P01", string(pqErr.Code), "transfer %d deadlocked across the 5-wallet lock graph: sorted wallet locking is broken", i)
			}
			t.Fatalf("transfer %d should not have failed: %v", i, err)
		}
		deltas[attempts[i].from] -= transferAmount
		deltas[attempts[i].to] += transferAmount
	}

	total := 0.0
	for i, walletID := range wallets {
		expected := startBalance + deltas[i]
		actual := getWalletBalance(t, db, walletID)
		assert.Equal(t, expected, actual, "wallet %d final balance should equal starting balance plus its net of all transfers", i)
		total += actual
	}
	assert.Equal(t, startBalance*numUsers, total, "total balance across all 5 wallets must be conserved")
}

// Goes through service.WalletService.TransferFunds - not the repo directly -
// because that's the actual entry point a client hits, and the
// retry/replay behavior under test (failureReasonError,
// replayIdempotentTransfer) lives in the service layer, not the repo.
//
// 5 users each fire one transfer concurrently; balances are set up so 2 of
// the 5 fail on insufficient balance and 3 succeed. Then every user
// manually retries with the exact same idempotency key and parameters -
// simulating a client resubmitting an identical request (e.g. a UI "Retry"
// button after seeing an error, or a network-timeout retry). A correct
// retry must never move funds a second time: the 3 successes replay their
// original transaction id/status, and the 2 failures surface the same
// specific ErrInsufficientBalance again rather than a generic conflict or,
// worse, silently re-attempting the debit.
func TestTransferFunds_FiveUsersSomeFailWithManualRetry(t *testing.T) {
	db := testDB(t)
	transferRepo := postgres.NewTransferRepository(db, db)
	walletSvc := &service.WalletService{TransferRepo: transferRepo}
	ctx := context.Background()

	type user struct {
		balance float64
		toIdx   int
		amount  float64
		wantErr error // nil if this transfer is expected to succeed
	}
	users := []user{
		{balance: 1000, toIdx: 1, amount: 100, wantErr: nil},                         // 0 -> 1: succeeds
		{balance: 50, toIdx: 2, amount: 200, wantErr: domain.ErrInsufficientBalance}, // 1 -> 2: fails
		{balance: 1000, toIdx: 3, amount: 100, wantErr: nil},                         // 2 -> 3: succeeds
		{balance: 30, toIdx: 4, amount: 500, wantErr: domain.ErrInsufficientBalance}, // 3 -> 4: fails
		{balance: 1000, toIdx: 0, amount: 100, wantErr: nil},                         // 4 -> 0: succeeds
	}

	wallets := make([]string, len(users))
	for i, u := range users {
		wallets[i] = createTestWallet(t, db, u.balance)
	}

	runToken := uniqueRunToken()
	keys := make([]string, len(users))
	for i := range users {
		keys[i] = fmt.Sprintf("manual-retry-%s-%d", runToken, i)
	}

	type result struct {
		transactionID string
		status        string
		err           error
	}

	runRound := func() []result {
		var wg sync.WaitGroup
		results := make([]result, len(users))
		for i, u := range users {
			wg.Add(1)
			go func(i int, u user) {
				defer wg.Done()
				txID, status, _, err := walletSvc.TransferFunds(ctx, keys[i], wallets[i], wallets[u.toIdx], u.amount)
				results[i] = result{txID, status, err}
			}(i, u)
		}
		wg.Wait()
		return results
	}

	first := runRound()
	for i, u := range users {
		if u.wantErr != nil {
			assert.ErrorIs(t, first[i].err, u.wantErr, "user %d initial attempt", i)
		} else {
			assert.NoError(t, first[i].err, "user %d initial attempt", i)
			assert.NotEmpty(t, first[i].transactionID, "user %d initial attempt", i)
		}
	}

	balancesAfterFirstRound := make([]float64, len(users))
	for i, w := range wallets {
		balancesAfterFirstRound[i] = getWalletBalance(t, db, w)
	}

	// Manual retry: identical idempotency key, from/to wallets, and amount.
	second := runRound()
	for i, u := range users {
		if u.wantErr != nil {
			assert.ErrorIs(t, second[i].err, u.wantErr, "user %d retry should surface the same specific error, not a generic conflict", i)
		} else {
			assert.NoError(t, second[i].err, "user %d retry", i)
			assert.Equal(t, first[i].transactionID, second[i].transactionID, "retry must return the original transaction, not create a new one")
			assert.Equal(t, first[i].status, second[i].status)
		}
	}

	// Balances must be identical before and after the retry round - whether
	// the original attempt succeeded or failed, a same-key retry must never
	// move funds a second time.
	for i, w := range wallets {
		assert.Equal(t, balancesAfterFirstRound[i], getWalletBalance(t, db, w), "wallet %d balance changed after retry - retry re-executed instead of replaying", i)
	}
}

func TestExecuteTransfer_ConcurrentSameIdempotencyKey_AppliesOnce(t *testing.T) {
	db := testDB(t)
	repo := postgres.NewTransferRepository(db, db)
	ctx := context.Background()

	const (
		numAttempts    = 20
		transferAmount = 25.0
		startBalance   = 1_000.0
	)

	fromWallet := createTestWallet(t, db, startBalance)
	toWallet := createTestWallet(t, db, 0)
	idempotencyKey := fmt.Sprintf("concurrent-idempotency-%s", t.Name())

	var wg sync.WaitGroup
	results := make([]error, numAttempts)

	for i := range numAttempts {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := repo.ExecuteTransfer(ctx, idempotencyKey, fromWallet, toWallet, transferAmount, int64(i))
			results[i] = err
		}(i)
	}
	wg.Wait()

	successes, conflicts := 0, 0
	for _, err := range results {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, domain.ErrIdempotencyKeyConflict):
			conflicts++
		default:
			t.Errorf("unexpected error: %v", err)
		}
	}

	assert.Equal(t, 1, successes, "exactly one attempt should have applied the transfer")
	assert.Equal(t, numAttempts-1, conflicts, "every other attempt should see the idempotency conflict")

	assert.Equal(t, startBalance-transferAmount, getWalletBalance(t, db, fromWallet))
	assert.Equal(t, transferAmount, getWalletBalance(t, db, toWallet))
}

func TestExecuteTransfer_InsufficientBalance_RecordsFailedTransferAndDedupesRetry(t *testing.T) {
	db := testDB(t)
	repo := postgres.NewTransferRepository(db, db)
	ctx := context.Background()

	const startBalance = 10.0
	const transferAmount = 100.0

	fromWallet := createTestWallet(t, db, startBalance)
	toWallet := createTestWallet(t, db, 0)
	idempotencyKey := fmt.Sprintf("insufficient-balance-%s", t.Name())

	transfer, err := repo.ExecuteTransfer(ctx, idempotencyKey, fromWallet, toWallet, transferAmount, 1)
	require.NoError(t, err, "insufficient balance is a recorded outcome, not a Go error")
	require.NotNil(t, transfer)
	assert.Equal(t, domain.TransferFailed, transfer.Status)
	require.NotNil(t, transfer.FailureReason)
	assert.Equal(t, domain.ErrInsufficientBalance.Error(), *transfer.FailureReason)

	assert.Equal(t, startBalance, getWalletBalance(t, db, fromWallet))
	assert.Equal(t, 0.0, getWalletBalance(t, db, toWallet))

	_, retryErr := repo.ExecuteTransfer(ctx, idempotencyKey, fromWallet, toWallet, transferAmount, 2)
	assert.ErrorIs(t, retryErr, domain.ErrIdempotencyKeyConflict)

	persisted, err := repo.GetByIdempotencyKey(ctx, idempotencyKey)
	require.NoError(t, err)
	require.NotNil(t, persisted)
	assert.Equal(t, domain.TransferFailed, persisted.Status)
}

func TestExecuteTransfer_ConcurrentInsufficientBalance_FailureRecordedOnce(t *testing.T) {
	db := testDB(t)
	repo := postgres.NewTransferRepository(db, db)
	ctx := context.Background()

	const (
		numAttempts    = 20
		startBalance   = 10.0
		transferAmount = 100.0
	)

	fromWallet := createTestWallet(t, db, startBalance)
	toWallet := createTestWallet(t, db, 0)
	idempotencyKey := fmt.Sprintf("concurrent-insufficient-balance-%s", t.Name())

	var wg sync.WaitGroup
	results := make([]error, numAttempts)
	transfers := make([]*domain.Transfer, numAttempts)

	for i := range numAttempts {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			transfer, err := repo.ExecuteTransfer(ctx, idempotencyKey, fromWallet, toWallet, transferAmount, int64(i))
			transfers[i] = transfer
			results[i] = err
		}(i)
	}
	wg.Wait()

	recorded, conflicts := 0, 0
	for i, err := range results {
		switch {
		case err == nil:
			recorded++
			require.NotNil(t, transfers[i])
			assert.Equal(t, domain.TransferFailed, transfers[i].Status)
		case errors.Is(err, domain.ErrIdempotencyKeyConflict):
			conflicts++
		default:
			t.Errorf("unexpected error: %v", err)
		}
	}

	assert.Equal(t, 1, recorded, "exactly one attempt should have recorded the failure")
	assert.Equal(t, numAttempts-1, conflicts, "every other attempt should see the idempotency conflict")

	assert.Equal(t, startBalance, getWalletBalance(t, db, fromWallet))
	assert.Equal(t, 0.0, getWalletBalance(t, db, toWallet))
}
