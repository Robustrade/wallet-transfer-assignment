package integration_test

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
	"golang.org/x/sync/errgroup"

	"github.com/benjamin-benny/wallet-transfer/internal/domain"
	"github.com/benjamin-benny/wallet-transfer/internal/service"
)

var (
	testPool *pgxpool.Pool
	testSvc  *service.TransferService
)

func TestMain(m *testing.M) {
	os.Exit(run(m))
}

func run(m *testing.M) int {
	ctx := context.Background()

	pgCtr, err := tcpostgres.Run(ctx, "postgres:16-alpine",
		tcpostgres.WithDatabase("testdb"),
		tcpostgres.WithUsername("test"),
		tcpostgres.WithPassword("test"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second),
		),
	)
	if err != nil {
		log.Fatalf("start postgres container: %v", err)
	}
	defer func() { _ = pgCtr.Terminate(ctx) }()

	connStr, err := pgCtr.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		log.Fatalf("get connection string: %v", err)
	}

	mgr, err := migrate.New("file://../../migrations", connStr)
	if err != nil {
		log.Fatalf("create migrator: %v", err)
	}
	if err := mgr.Up(); err != nil && err != migrate.ErrNoChange {
		log.Fatalf("run migrations: %v", err)
	}

	testPool, err = pgxpool.New(ctx, connStr)
	if err != nil {
		log.Fatalf("create pool: %v", err)
	}
	defer testPool.Close()

	testSvc = service.NewTransferService(testPool)

	return m.Run()
}

// ── Test 1: Happy path ───────────────────────────────────────────────────────

func TestHappyPath(t *testing.T) {
	ctx := context.Background()

	walletA := createWallet(t, "USD", "100.0000")
	walletB := createWallet(t, "USD", "50.0000")

	result, err := testSvc.Create(ctx, domain.CreateTransferRequest{
		IdempotencyKey: uuid.NewString(),
		FromWalletID:   walletA.ID,
		ToWalletID:     walletB.ID,
		Amount:         mustAmount("30.0000"),
	})
	require.NoError(t, err)

	assert.Equal(t, http.StatusCreated, result.HTTPStatus)
	assert.Equal(t, "PROCESSED", result.Response.Status)
	assert.Equal(t, "30.0000", result.Response.Amount)
	assert.NotEmpty(t, result.Response.TransferID)
	assert.NotNil(t, result.Response.ProcessedAt)

	// Balances updated.
	assert.Equal(t, "70.0000", dbBalance(t, walletA.ID))
	assert.Equal(t, "80.0000", dbBalance(t, walletB.ID))

	// Exactly two ledger entries for this transfer.
	transferID := result.Response.TransferID
	assert.Equal(t, 2, countLedgerForTransfer(t, transferID))

	// Ledger is balanced: DEBIT amount == CREDIT amount.
	assert.True(t, ledgerIsBalanced(t, transferID), "ledger entries should balance")
}

// ── Test 2: Idempotency replay ───────────────────────────────────────────────

func TestIdempotencyReplay(t *testing.T) {
	ctx := context.Background()

	walletA := createWallet(t, "USD", "200.0000")
	walletB := createWallet(t, "USD", "0.0000")
	key := uuid.NewString()

	req := domain.CreateTransferRequest{
		IdempotencyKey: key,
		FromWalletID:   walletA.ID,
		ToWalletID:     walletB.ID,
		Amount:         mustAmount("50.0000"),
	}

	first, err := testSvc.Create(ctx, req)
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, first.HTTPStatus)

	// Second call with identical request.
	second, err := testSvc.Create(ctx, req)
	require.NoError(t, err)

	// The service stores 201 and returns IsReplay=true; the HTTP handler maps that to 200.
	// We replicate that logic here to keep the test at the service layer.
	assert.Equal(t, http.StatusOK, resolvedHTTPStatus(second), "replay must resolve to 200")
	assert.True(t, second.IsReplay)
	assert.Equal(t, first.Response.TransferID, second.Response.TransferID, "transfer ID must be identical")
	assert.Equal(t, first.Response.Status, second.Response.Status)

	// Balances unchanged after the second call.
	assert.Equal(t, "150.0000", dbBalance(t, walletA.ID))
	assert.Equal(t, "50.0000", dbBalance(t, walletB.ID))

	// Only one transfer row in the DB.
	assert.Equal(t, 1, countTransfersForWallets(t, walletA.ID, walletB.ID))
}

// ── Test 3: Idempotency conflict ─────────────────────────────────────────────

func TestIdempotencyConflict(t *testing.T) {
	ctx := context.Background()

	walletA := createWallet(t, "USD", "100.0000")
	walletB := createWallet(t, "USD", "0.0000")
	key := uuid.NewString()

	_, err := testSvc.Create(ctx, domain.CreateTransferRequest{
		IdempotencyKey: key,
		FromWalletID:   walletA.ID,
		ToWalletID:     walletB.ID,
		Amount:         mustAmount("10.0000"),
	})
	require.NoError(t, err)

	// Same key, different amount.
	_, err = testSvc.Create(ctx, domain.CreateTransferRequest{
		IdempotencyKey: key,
		FromWalletID:   walletA.ID,
		ToWalletID:     walletB.ID,
		Amount:         mustAmount("99.0000"),
	})
	require.ErrorIs(t, err, domain.ErrIdempotencyConflict)
}

// ── Test 4: Insufficient funds ───────────────────────────────────────────────

func TestInsufficientFunds(t *testing.T) {
	ctx := context.Background()

	walletA := createWallet(t, "USD", "10.0000")
	walletB := createWallet(t, "USD", "0.0000")

	result, err := testSvc.Create(ctx, domain.CreateTransferRequest{
		IdempotencyKey: uuid.NewString(),
		FromWalletID:   walletA.ID,
		ToWalletID:     walletB.ID,
		Amount:         mustAmount("50.0000"),
	})
	require.NoError(t, err)

	assert.Equal(t, http.StatusUnprocessableEntity, result.HTTPStatus)
	assert.Equal(t, "FAILED", result.Response.Status)

	// Transfer recorded as FAILED in the DB.
	status, failureReason := dbTransferStatus(t, result.Response.TransferID)
	assert.Equal(t, "FAILED", status)
	assert.Contains(t, failureReason, "insufficient")

	// No ledger entries for a failed transfer.
	assert.Equal(t, 0, countLedgerForTransfer(t, result.Response.TransferID))

	// Balances unchanged.
	assert.Equal(t, "10.0000", dbBalance(t, walletA.ID))
	assert.Equal(t, "0.0000", dbBalance(t, walletB.ID))
}

// ── Test 4b: Idempotent replay of a failed transfer ──────────────────────────

func TestInsufficientFundsReplay(t *testing.T) {
	ctx := context.Background()

	walletA := createWallet(t, "USD", "5.0000")
	walletB := createWallet(t, "USD", "0.0000")
	key := uuid.NewString()

	req := domain.CreateTransferRequest{
		IdempotencyKey: key,
		FromWalletID:   walletA.ID,
		ToWalletID:     walletB.ID,
		Amount:         mustAmount("100.0000"),
	}

	first, err := testSvc.Create(ctx, req)
	require.NoError(t, err)
	require.Equal(t, http.StatusUnprocessableEntity, first.HTTPStatus)

	// Replay must return the same 422.
	second, err := testSvc.Create(ctx, req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusUnprocessableEntity, second.HTTPStatus)
	assert.True(t, second.IsReplay)
	assert.Equal(t, first.Response.TransferID, second.Response.TransferID)
}

// ── Test 5: Self-transfer rejected ───────────────────────────────────────────

func TestSelfTransferRejected(t *testing.T) {
	ctx := context.Background()

	wallet := createWallet(t, "USD", "100.0000")

	_, err := testSvc.Create(ctx, domain.CreateTransferRequest{
		IdempotencyKey: uuid.NewString(),
		FromWalletID:   wallet.ID,
		ToWalletID:     wallet.ID,
		Amount:         mustAmount("10.0000"),
	})
	require.ErrorIs(t, err, domain.ErrSameWallet)
}

// ── Test 6: Zero / negative amount rejected ───────────────────────────────────

func TestZeroAmountRejected(t *testing.T) {
	ctx := context.Background()

	walletA := createWallet(t, "USD", "100.0000")
	walletB := createWallet(t, "USD", "0.0000")

	t.Run("zero", func(t *testing.T) {
		_, err := testSvc.Create(ctx, domain.CreateTransferRequest{
			IdempotencyKey: uuid.NewString(),
			FromWalletID:   walletA.ID,
			ToWalletID:     walletB.ID,
			Amount:         mustAmount("0"),
		})
		require.ErrorIs(t, err, domain.ErrInvalidAmount)
	})
}

func TestNegativeAmountRejectedAtParse(t *testing.T) {
	// Negative amounts are rejected by ParseAmount before reaching the service.
	_, err := domain.ParseAmount("-1.0000")
	require.Error(t, err)
}

// ── Test 7: Concurrency ──────────────────────────────────────────────────────

func TestConcurrentTransfers(t *testing.T) {
	ctx := context.Background()

	// Wallet A starts with ₹50; 10 goroutines each try to transfer ₹10.
	// Exactly 5 should succeed before the balance hits 0.
	walletA := createWallet(t, "INR", "50.0000")
	walletB := createWallet(t, "INR", "0.0000")

	var mu sync.Mutex
	var successCount, failCount int

	g, gctx := errgroup.WithContext(ctx)
	for i := range 10 {
		i := i
		g.Go(func() error {
			result, err := testSvc.Create(gctx, domain.CreateTransferRequest{
				IdempotencyKey: fmt.Sprintf("concurrent-%s-%d", walletA.ID, i),
				FromWalletID:   walletA.ID,
				ToWalletID:     walletB.ID,
				Amount:         mustAmount("10.0000"),
			})
			if err != nil {
				return err
			}
			mu.Lock()
			defer mu.Unlock()
			switch result.HTTPStatus {
			case http.StatusCreated:
				successCount++
			case http.StatusUnprocessableEntity:
				failCount++
			}
			return nil
		})
	}

	require.NoError(t, g.Wait())

	assert.Equal(t, 5, successCount, "expected exactly 5 successful transfers")
	assert.Equal(t, 5, failCount, "expected exactly 5 failed transfers")

	// Final balances: A drained to 0, B received 5×₹10 = 50.
	assert.Equal(t, "0.0000", dbBalance(t, walletA.ID))
	assert.Equal(t, "50.0000", dbBalance(t, walletB.ID))

	// 5 successful transfers × 2 entries each = 10 ledger entries.
	assert.Equal(t, 5, countLedgerByWalletAndType(t, walletA.ID, "DEBIT"))
	assert.Equal(t, 5, countLedgerByWalletAndType(t, walletB.ID, "CREDIT"))
}

// ── Test 8: Ledger invariant ─────────────────────────────────────────────────

func TestLedgerInvariant(t *testing.T) {
	ctx := context.Background()

	// Create a ring of wallets, each starting with 100.
	const numWallets = 5
	wallets := make([]*domain.Wallet, numWallets)
	for i := range wallets {
		wallets[i] = createWallet(t, "USD", "100.0000")
	}

	initialTotal := mustAmount(fmt.Sprintf("%d.0000", numWallets*100))

	// Run transfers in a round-robin pattern with varying amounts.
	amounts := []string{"10.0000", "20.0000", "5.0000", "30.0000", "15.0000"}
	var successfulTransferIDs []string
	for round := range 4 {
		for i := range wallets {
			from := wallets[i]
			to := wallets[(i+1)%numWallets]
			amount := amounts[(round*numWallets+i)%len(amounts)]

			result, err := testSvc.Create(ctx, domain.CreateTransferRequest{
				IdempotencyKey: fmt.Sprintf("invariant-%s-%d-%d", from.ID, round, i),
				FromWalletID:   from.ID,
				ToWalletID:     to.ID,
				Amount:         mustAmount(amount),
			})
			require.NoError(t, err)
			if result.HTTPStatus == http.StatusCreated {
				successfulTransferIDs = append(successfulTransferIDs, result.Response.TransferID)
			}
		}
	}

	// Invariant 1: per successful transfer, DEBIT amount == CREDIT amount.
	for _, tid := range successfulTransferIDs {
		assert.True(t, ledgerIsBalanced(t, tid),
			"transfer %s: ledger entries do not balance", tid)
	}

	// Invariant 2: total balance across all wallets equals initial total.
	walletIDs := make([]uuid.UUID, numWallets)
	for i, w := range wallets {
		walletIDs[i] = w.ID
	}
	actualTotal := sumBalances(t, walletIDs)
	assert.Equal(t, initialTotal.String(), actualTotal,
		"total balance must be conserved after all transfers")
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// resolvedHTTPStatus mirrors the handler's 201→200 conversion for idempotent replays.
func resolvedHTTPStatus(r *service.CreateResult) int {
	if r.IsReplay && r.HTTPStatus == http.StatusCreated {
		return http.StatusOK
	}
	return r.HTTPStatus
}

func mustAmount(s string) domain.Amount {
	a, err := domain.ParseAmount(s)
	if err != nil {
		panic(fmt.Sprintf("mustAmount(%q): %v", s, err))
	}
	return a
}

func createWallet(t *testing.T, currency, balance string) *domain.Wallet {
	t.Helper()
	w, err := testSvc.CreateWallet(context.Background(), currency, mustAmount(balance))
	require.NoError(t, err)
	return w
}

// dbBalance fetches the current balance for a wallet directly from the DB.
func dbBalance(t *testing.T, id uuid.UUID) string {
	t.Helper()
	var balance string
	err := testPool.QueryRow(context.Background(),
		"SELECT balance::text FROM wallets WHERE id = $1", id,
	).Scan(&balance)
	require.NoError(t, err)
	// Normalise to 4 decimal places via domain Amount.
	a, err := domain.ParseAmount(balance)
	require.NoError(t, err)
	return a.String()
}

// dbTransferStatus fetches status and failure_reason for a transfer.
func dbTransferStatus(t *testing.T, transferID string) (status, failureReason string) {
	t.Helper()
	var fr *string
	err := testPool.QueryRow(context.Background(),
		"SELECT status::text, failure_reason FROM transfers WHERE id = $1", transferID,
	).Scan(&status, &fr)
	require.NoError(t, err)
	if fr != nil {
		failureReason = *fr
	}
	return status, failureReason
}

// countLedgerForTransfer counts ledger_entries rows for a given transfer UUID.
func countLedgerForTransfer(t *testing.T, transferID string) int {
	t.Helper()
	var n int
	err := testPool.QueryRow(context.Background(),
		"SELECT COUNT(*) FROM ledger_entries WHERE transfer_id = $1", transferID,
	).Scan(&n)
	require.NoError(t, err)
	return n
}

// ledgerIsBalanced checks that a transfer's DEBIT and CREDIT amounts are equal.
func ledgerIsBalanced(t *testing.T, transferID string) bool {
	t.Helper()
	var unbalanced int
	err := testPool.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM (
			SELECT transfer_id
			FROM ledger_entries
			WHERE transfer_id = $1
			GROUP BY transfer_id
			HAVING SUM(CASE WHEN entry_type = 'DEBIT'  THEN amount ELSE 0 END) <>
			       SUM(CASE WHEN entry_type = 'CREDIT' THEN amount ELSE 0 END)
		) sub`, transferID,
	).Scan(&unbalanced)
	require.NoError(t, err)
	return unbalanced == 0
}

// countTransfersForWallets counts transfers between two specific wallets.
func countTransfersForWallets(t *testing.T, from, to uuid.UUID) int {
	t.Helper()
	var n int
	err := testPool.QueryRow(context.Background(),
		"SELECT COUNT(*) FROM transfers WHERE from_wallet_id = $1 AND to_wallet_id = $2",
		from, to,
	).Scan(&n)
	require.NoError(t, err)
	return n
}

// countLedgerByWalletAndType counts ledger entries for a wallet filtered by entry type.
func countLedgerByWalletAndType(t *testing.T, walletID uuid.UUID, entryType string) int {
	t.Helper()
	var n int
	err := testPool.QueryRow(context.Background(),
		"SELECT COUNT(*) FROM ledger_entries WHERE wallet_id = $1 AND entry_type = $2",
		walletID, entryType,
	).Scan(&n)
	require.NoError(t, err)
	return n
}

// sumBalances returns the sum of balances for a list of wallets as a formatted string.
func sumBalances(t *testing.T, ids []uuid.UUID) string {
	t.Helper()
	var total string
	err := testPool.QueryRow(context.Background(),
		"SELECT COALESCE(SUM(balance), 0)::text FROM wallets WHERE id = ANY($1)", ids,
	).Scan(&total)
	require.NoError(t, err)
	a, err := domain.ParseAmount(total)
	require.NoError(t, err)
	return a.String()
}
