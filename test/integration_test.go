//go:build integration

// Package integration exercises the real Postgres-backed stack: actual row
// locking, actual constraints, actual concurrent transactions. Run with
// `make test-integration` (requires `make db-up` / docker compose).
package integration

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/robustrade/wallet-transfer-assignment/internal/db"
	"github.com/robustrade/wallet-transfer-assignment/internal/domain"
	"github.com/robustrade/wallet-transfer-assignment/internal/repository/postgres"
	"github.com/robustrade/wallet-transfer-assignment/internal/service"
)

func setup(t *testing.T) (*service.TransferService, *service.WalletService) {
	t.Helper()
	dsn := os.Getenv("INTEGRATION_DATABASE_URL")
	if dsn == "" {
		t.Skip("INTEGRATION_DATABASE_URL not set, skipping integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := db.Connect(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	require.NoError(t, db.Migrate(ctx, pool))

	walletRepo := postgres.NewWalletRepository(pool)
	transferRepo := postgres.NewTransferRepository(pool)
	ledgerRepo := postgres.NewLedgerRepository(pool)
	idemRepo := postgres.NewIdempotencyRepository(pool)
	tx := postgres.NewTxManager(pool)

	transferSvc := service.NewTransferService(tx, walletRepo, transferRepo, ledgerRepo, idemRepo)
	walletSvc := service.NewWalletService(walletRepo, ledgerRepo)
	return transferSvc, walletSvc
}

func uniqueID(prefix string) string {
	return prefix + "_" + uuid.NewString()[:8]
}

func TestIntegration_TransferEndToEnd(t *testing.T) {
	transferSvc, walletSvc := setup(t)
	ctx := context.Background()

	from := uniqueID("wallet")
	to := uniqueID("wallet")
	_, err := walletSvc.CreateWallet(ctx, service.CreateWalletInput{ID: from, Balance: 1000, Currency: "USD"})
	require.NoError(t, err)
	_, err = walletSvc.CreateWallet(ctx, service.CreateWalletInput{ID: to, Balance: 0, Currency: "USD"})
	require.NoError(t, err)

	result, err := transferSvc.CreateTransfer(ctx, service.CreateTransferInput{
		IdempotencyKey: uniqueID("idem"), FromWalletID: from, ToWalletID: to, Amount: 250,
	})
	require.NoError(t, err)
	require.Equal(t, domain.TransferProcessed, result.Status)

	fromWallet, err := walletSvc.GetWallet(ctx, from)
	require.NoError(t, err)
	toWallet, err := walletSvc.GetWallet(ctx, to)
	require.NoError(t, err)
	require.EqualValues(t, 750, fromWallet.Balance)
	require.EqualValues(t, 250, toWallet.Balance)

	entries, err := walletSvc.ListLedger(ctx, from)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, domain.EntryDebit, entries[0].EntryType)
}

func TestIntegration_ReplayReturnsSameTransfer(t *testing.T) {
	transferSvc, walletSvc := setup(t)
	ctx := context.Background()

	from := uniqueID("wallet")
	to := uniqueID("wallet")
	_, err := walletSvc.CreateWallet(ctx, service.CreateWalletInput{ID: from, Balance: 500, Currency: "USD"})
	require.NoError(t, err)
	_, err = walletSvc.CreateWallet(ctx, service.CreateWalletInput{ID: to, Balance: 0, Currency: "USD"})
	require.NoError(t, err)

	in := service.CreateTransferInput{IdempotencyKey: uniqueID("idem"), FromWalletID: from, ToWalletID: to, Amount: 100}

	first, err := transferSvc.CreateTransfer(ctx, in)
	require.NoError(t, err)

	second, err := transferSvc.CreateTransfer(ctx, in)
	require.NoError(t, err)
	require.True(t, second.Replayed)
	require.Equal(t, first.TransferID, second.TransferID)

	fromWallet, err := walletSvc.GetWallet(ctx, from)
	require.NoError(t, err)
	require.EqualValues(t, 400, fromWallet.Balance, "replay must not move money twice, even across process/DB round trips")
}

// TestIntegration_ConcurrentDuplicateIdempotencyKey fires the same
// idempotency key from many real goroutines, each with its own DB
// transaction, at once. This is the scenario a naive "check then insert"
// idempotency implementation gets wrong under real concurrency.
func TestIntegration_ConcurrentDuplicateIdempotencyKey(t *testing.T) {
	transferSvc, walletSvc := setup(t)
	ctx := context.Background()

	from := uniqueID("wallet")
	to := uniqueID("wallet")
	_, err := walletSvc.CreateWallet(ctx, service.CreateWalletInput{ID: from, Balance: 1000, Currency: "USD"})
	require.NoError(t, err)
	_, err = walletSvc.CreateWallet(ctx, service.CreateWalletInput{ID: to, Balance: 0, Currency: "USD"})
	require.NoError(t, err)

	key := uniqueID("idem")
	const n = 10
	var wg sync.WaitGroup
	results := make([]*service.TransferResult, n)
	errs := make([]error, n)

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = transferSvc.CreateTransfer(ctx, service.CreateTransferInput{
				IdempotencyKey: key, FromWalletID: from, ToWalletID: to, Amount: 100,
			})
		}(i)
	}
	wg.Wait()

	for i := 0; i < n; i++ {
		if errs[i] != nil {
			require.ErrorIs(t, errs[i], domain.ErrRequestInProgress)
		}
	}

	fromWallet, err := walletSvc.GetWallet(ctx, from)
	require.NoError(t, err)
	require.EqualValues(t, 900, fromWallet.Balance, "duplicate concurrent requests with the same key must move money exactly once")
}

// TestIntegration_ConcurrentTransfersNoDoubleSpend fires more concurrent
// transfer demand at a single source wallet than it can afford, each with a
// distinct idempotency key and destination. Without correct row-level
// locking this either double-spends (balance goes negative / more transfers
// succeed than the balance allows) or loses updates (balance ends up wrong).
// With SELECT ... FOR UPDATE, exactly floor(balance/amount) succeed.
func TestIntegration_ConcurrentTransfersNoDoubleSpend(t *testing.T) {
	transferSvc, walletSvc := setup(t)
	ctx := context.Background()

	source := uniqueID("wallet")
	_, err := walletSvc.CreateWallet(ctx, service.CreateWalletInput{ID: source, Balance: 1000, Currency: "USD"})
	require.NoError(t, err)

	const n = 15
	const amount = 100 // demand = 1500 against a balance of 1000

	destinations := make([]string, n)
	for i := range destinations {
		destinations[i] = uniqueID("wallet")
		_, err := walletSvc.CreateWallet(ctx, service.CreateWalletInput{ID: destinations[i], Balance: 0, Currency: "USD"})
		require.NoError(t, err)
	}

	var wg sync.WaitGroup
	results := make([]*service.TransferResult, n)
	errs := make([]error, n)

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = transferSvc.CreateTransfer(ctx, service.CreateTransferInput{
				IdempotencyKey: uniqueID("idem"),
				FromWalletID:   source,
				ToWalletID:     destinations[i],
				Amount:         amount,
			})
		}(i)
	}
	wg.Wait()

	processed := 0
	for i := 0; i < n; i++ {
		require.NoError(t, errs[i])
		if results[i].Status == domain.TransferProcessed {
			processed++
		} else {
			require.Equal(t, domain.TransferFailed, results[i].Status)
		}
	}

	sourceWallet, err := walletSvc.GetWallet(ctx, source)
	require.NoError(t, err)
	require.GreaterOrEqual(t, sourceWallet.Balance, int64(0), "balance must never go negative")
	require.EqualValues(t, 1000-int64(processed*amount), sourceWallet.Balance)
	require.Equal(t, 10, processed, "exactly floor(balance/amount) transfers should succeed under contention")
}

// TestIntegration_ConcurrentMigrationsAreSafe reproduces the scenario several
// replicas of this service starting at the same time would hit: multiple
// processes calling db.Migrate against the same database concurrently. A
// naive "check schema_migrations, then INSERT" is unsafe here -- two
// processes can both see a migration as unapplied before either commits, and
// the loser's INSERT fails on the primary key, crashing startup even though
// the migration itself is idempotent. Migrate serializes this with a
// Postgres advisory lock, so every concurrent caller should succeed.
func TestIntegration_ConcurrentMigrationsAreSafe(t *testing.T) {
	dsn := os.Getenv("INTEGRATION_DATABASE_URL")
	if dsn == "" {
		t.Skip("INTEGRATION_DATABASE_URL not set, skipping integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	pool, err := db.Connect(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	const n = 8
	var wg sync.WaitGroup
	errs := make([]error, n)

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = db.Migrate(ctx, pool)
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		require.NoError(t, err, "concurrent Migrate() call %d should not fail", i)
	}
}

// TestIntegration_RollbackSurvivesCancelledContext proves that a failed
// transaction still rolls back cleanly -- with no side effect on the
// reported error -- even when the caller's context is already cancelled by
// the time WithinTx tries to clean up (e.g. an HTTP client disconnecting
// mid-request). cancel() is called synchronously inside fn, right before it
// returns its error, so the original ctx is guaranteed to be dead by the
// time WithinTx's rollback path runs -- no timing race.
//
// Note on what this does and doesn't prove: pgxpool discards a connection
// outright rather than pooling it after a failed operation, and Postgres
// itself rolls back any open transaction when the underlying connection
// closes -- so using the cancelled ctx for Rollback does not actually leak a
// permanently-open "idle in transaction" session (verified manually against
// pg_stat_activity while developing this fix). The concrete, demonstrable
// bug is narrower but still real: Rollback(ctx) fails fast on an already
// -cancelled ctx, and that failure gets wrapped onto the real error as a
// misleading "(rollback also failed: context canceled)" suffix -- which
// pollutes error logs with a rollback "failure" that was never actually
// about the rollback, and burns a connection (forcing a reconnect) for no
// reason beyond having used the wrong context.
func TestIntegration_RollbackSurvivesCancelledContext(t *testing.T) {
	dsn := os.Getenv("INTEGRATION_DATABASE_URL")
	if dsn == "" {
		t.Skip("INTEGRATION_DATABASE_URL not set, skipping integration test")
	}

	setupCtx, setupCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer setupCancel()

	pool, err := db.Connect(setupCtx, dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	txManager := postgres.NewTxManager(pool)

	ctx, cancel := context.WithCancel(context.Background())
	sentinelErr := errors.New("boom")

	runErr := txManager.WithinTx(ctx, func(txCtx context.Context) error {
		cancel() // cancel the ORIGINAL ctx deterministically, before returning
		return sentinelErr
	})

	require.ErrorIs(t, runErr, sentinelErr)
	require.Equal(t, sentinelErr.Error(), runErr.Error(),
		"the original error must propagate byte-for-byte -- not get a misleading "+
			"'(rollback also failed: ...)' suffix appended just because rollback used an "+
			"already-cancelled context instead of its own")
}

// TestIntegration_IdempotencyRecordCompletedRequiresTransferID proves the
// schema itself refuses an inconsistent state: a COMPLETED idempotency
// record with no linked transfer. The application code never does this
// today, but nothing in Go stops a future change from doing it by mistake --
// the CHECK constraint is the backstop.
func TestIntegration_IdempotencyRecordCompletedRequiresTransferID(t *testing.T) {
	dsn := os.Getenv("INTEGRATION_DATABASE_URL")
	if dsn == "" {
		t.Skip("INTEGRATION_DATABASE_URL not set, skipping integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := db.Connect(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	require.NoError(t, db.Migrate(ctx, pool))

	_, err = pool.Exec(ctx, `
		INSERT INTO idempotency_records (idempotency_key, request_hash, status)
		VALUES ($1, 'somehash', 'COMPLETED')
	`, uniqueID("idem"))
	require.Error(t, err, "COMPLETED with a NULL transfer_id must be rejected by the check constraint")

	// A PENDING record with no transfer_id yet -- the normal, expected state
	// right after Claim -- must still be allowed.
	_, err = pool.Exec(ctx, `
		INSERT INTO idempotency_records (idempotency_key, request_hash, status)
		VALUES ($1, 'somehash', 'PENDING')
	`, uniqueID("idem"))
	require.NoError(t, err, "PENDING with no transfer_id yet must still be allowed")
}
