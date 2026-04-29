package service_test

import (
	"context"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"wallet-transfer/internal/transfer"
	transferRepo "wallet-transfer/internal/transfer/repository"
	transferService "wallet-transfer/internal/transfer/service"
	"wallet-transfer/internal/transfer/static"
	walletRepo "wallet-transfer/internal/wallet/repository"
	walletService "wallet-transfer/internal/wallet/service"
	"wallet-transfer/pkg/database"
	"wallet-transfer/pkg/logger"
)

// ─── Setup ────────────────────────────────────────────────────────────────────

const defaultDSN = "postgres://wallet_user:wallet_pass@localhost:5432/wallet_db"

func integrationDSN() string {
	if v := os.Getenv("TEST_DB_URL"); v != "" {
		return v
	}
	return defaultDSN
}

// setupIntegration connects to the real DB and returns a teardown func.
func setupIntegration(t *testing.T) (*pgxpool.Pool, func()) {
	t.Helper()
	logger.Init(0)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := database.Connect(ctx, integrationDSN()); err != nil {
		t.Skipf("skipping integration tests — DB unavailable: %v", err)
	}

	pool := database.GetPool()

	teardown := func() {
		ctx := context.Background()
		// Delete all ledger entries linked to integ wallets (covers keyed and keyless transfers).
		if _, err := pool.Exec(ctx, `DELETE FROM ledger_entries WHERE wallet_id LIKE 'integ-%'`); err != nil {
			t.Logf("teardown: failed to delete ledger_entries: %v", err)
		}
		// Delete all transfers involving integ wallets.
		if _, err := pool.Exec(ctx, `DELETE FROM transfers WHERE from_wallet_id LIKE 'integ-%' OR to_wallet_id LIKE 'integ-%'`); err != nil {
			t.Logf("teardown: failed to delete transfers: %v", err)
		}
		if _, err := pool.Exec(ctx, `DELETE FROM wallets WHERE id LIKE 'integ-%'`); err != nil {
			t.Logf("teardown: failed to delete wallets: %v", err)
		}
		pool.Close()
	}

	return pool, teardown
}

// seedWallet creates a wallet in the DB and returns it.
func seedWallet(t *testing.T, pool *pgxpool.Pool, id string, balance float64) {
	t.Helper()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO wallets (id, balance) VALUES ($1, $2) ON CONFLICT (id) DO UPDATE SET balance = $2`,
		id, balance,
	)
	require.NoError(t, err)
}

// getBalance fetches the current balance of a wallet directly from DB.
func getBalance(t *testing.T, pool *pgxpool.Pool, walletID string) float64 {
	t.Helper()
	var bal float64
	err := pool.QueryRow(context.Background(),
		`SELECT balance FROM wallets WHERE id = $1`, walletID,
	).Scan(&bal)
	require.NoError(t, err)
	return bal
}

// countLedgerEntries returns the number of ledger entries for a transfer.
func countLedgerEntries(t *testing.T, pool *pgxpool.Pool, transferID string) int {
	t.Helper()
	var count int
	err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM ledger_entries WHERE transfer_id = $1`, transferID,
	).Scan(&count)
	require.NoError(t, err)
	return count
}

// newSvc builds a real TransferService wired to the live DB pool.
func newSvc(pool *pgxpool.Pool) transferService.TransferService {
	wr := walletRepo.NewWalletRepository()
	tr := transferRepo.NewTransferRepository()
	return transferService.NewTransferService(pool, tr, wr)
}

// ─── Integration: happy path ──────────────────────────────────────────────────

func TestIntegration_CreateTransfer_Success(t *testing.T) {
	pool, teardown := setupIntegration(t)
	defer teardown()

	fromID := "integ-from-" + uuid.New().String()[:8]
	toID := "integ-to-" + uuid.New().String()[:8]
	seedWallet(t, pool, fromID, 500)
	seedWallet(t, pool, toID, 100)

	svc := newSvc(pool)
	resp, err := svc.CreateTransfer(context.Background(), &transfer.CreateTransferRequest{
		IdempotencyKey: "integ-ok-1",
		FromWalletID:   fromID,
		ToWalletID:     toID,
		Amount:         200,
	})

	require.NoError(t, err)
	assert.Equal(t, static.StatusProcessed, resp.Transfer.Status)

	// Balances updated correctly.
	assert.Equal(t, 300.0, getBalance(t, pool, fromID))
	assert.Equal(t, 300.0, getBalance(t, pool, toID))

	// Exactly 2 ledger entries created.
	assert.Equal(t, 2, countLedgerEntries(t, pool, resp.Transfer.ID))

	// One DEBIT, one CREDIT.
	var debits, credits int
	for _, e := range resp.Ledger {
		if e.Type == static.TypeDebit {
			debits++
		} else {
			credits++
		}
	}
	assert.Equal(t, 1, debits, "expected 1 DEBIT entry")
	assert.Equal(t, 1, credits, "expected 1 CREDIT entry")
}

// ─── Integration: idempotency ─────────────────────────────────────────────────

func TestIntegration_Idempotency_DuplicateReturnsSameResult(t *testing.T) {
	pool, teardown := setupIntegration(t)
	defer teardown()

	fromID := "integ-idem-from-" + uuid.New().String()[:8]
	toID := "integ-idem-to-" + uuid.New().String()[:8]
	seedWallet(t, pool, fromID, 500)
	seedWallet(t, pool, toID, 0)

	svc := newSvc(pool)
	req := &transfer.CreateTransferRequest{
		IdempotencyKey: "integ-idem-key-1",
		FromWalletID:   fromID,
		ToWalletID:     toID,
		Amount:         100,
	}

	// First call — should execute the transfer.
	resp1, err := svc.CreateTransfer(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, static.StatusProcessed, resp1.Transfer.Status)

	balAfterFirst := getBalance(t, pool, fromID)
	assert.Equal(t, 400.0, balAfterFirst)

	// Second call with the same idempotency key — must NOT re-execute.
	resp2, err := svc.CreateTransfer(context.Background(), req)
	require.NoError(t, err)

	// Same transfer ID returned.
	assert.Equal(t, resp1.Transfer.ID, resp2.Transfer.ID)

	// Balance must not change — no second debit.
	assert.Equal(t, balAfterFirst, getBalance(t, pool, fromID), "balance must not change on duplicate request")

	// Still exactly 2 ledger entries (not 4).
	assert.Equal(t, 2, countLedgerEntries(t, pool, resp1.Transfer.ID))
}

// ─── Integration: insufficient balance ───────────────────────────────────────

func TestIntegration_InsufficientBalance_TransferFails(t *testing.T) {
	pool, teardown := setupIntegration(t)
	defer teardown()

	fromID := "integ-insuf-from-" + uuid.New().String()[:8]
	toID := "integ-insuf-to-" + uuid.New().String()[:8]
	seedWallet(t, pool, fromID, 50)
	seedWallet(t, pool, toID, 0)

	svc := newSvc(pool)
	_, err := svc.CreateTransfer(context.Background(), &transfer.CreateTransferRequest{
		IdempotencyKey: "integ-insuf-1",
		FromWalletID:   fromID,
		ToWalletID:     toID,
		Amount:         200, // more than balance
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), static.InsufficientBalance)

	// Balance must remain unchanged.
	assert.Equal(t, 50.0, getBalance(t, pool, fromID))
	assert.Equal(t, 0.0, getBalance(t, pool, toID))
}

// ─── Integration: wallet not found ───────────────────────────────────────────

func TestIntegration_WalletNotFound_TransferFails(t *testing.T) {
	pool, teardown := setupIntegration(t)
	defer teardown()

	realWalletID := "integ-real-" + uuid.New().String()[:8]
	seedWallet(t, pool, realWalletID, 500)

	svc := newSvc(pool)
	_, err := svc.CreateTransfer(context.Background(), &transfer.CreateTransferRequest{
		FromWalletID: realWalletID,
		ToWalletID:   "integ-ghost-wallet-does-not-exist",
		Amount:       100,
	})

	require.Error(t, err)
}

// ─── Integration: ledger balance ─────────────────────────────────────────────

func TestIntegration_LedgerIsBalanced(t *testing.T) {
	pool, teardown := setupIntegration(t)
	defer teardown()

	fromID := "integ-ledger-from-" + uuid.New().String()[:8]
	toID := "integ-ledger-to-" + uuid.New().String()[:8]
	seedWallet(t, pool, fromID, 1000)
	seedWallet(t, pool, toID, 0)

	svc := newSvc(pool)

	// Execute 3 transfers.
	amounts := []float64{100, 250, 75}
	for i, amt := range amounts {
		_, err := svc.CreateTransfer(context.Background(), &transfer.CreateTransferRequest{
			IdempotencyKey: fmt.Sprintf("integ-ledger-%d", i),
			FromWalletID:   fromID,
			ToWalletID:     toID,
			Amount:         amt,
		})
		require.NoError(t, err)
	}

	// Sum of all DEBIT entries for fromID must equal sum of all CREDIT entries for toID.
	var totalDebited, totalCredited float64
	pool.QueryRow(context.Background(),
		`SELECT COALESCE(SUM(amount),0) FROM ledger_entries WHERE wallet_id = $1 AND type = 'DEBIT'`,
		fromID,
	).Scan(&totalDebited) //nolint:errcheck
	pool.QueryRow(context.Background(),
		`SELECT COALESCE(SUM(amount),0) FROM ledger_entries WHERE wallet_id = $1 AND type = 'CREDIT'`,
		toID,
	).Scan(&totalCredited) //nolint:errcheck

	assert.Equal(t, 425.0, totalDebited)
	assert.Equal(t, 425.0, totalCredited)
	assert.Equal(t, totalDebited, totalCredited, "ledger must be balanced")
}

// ─── Integration: concurrency safety ─────────────────────────────────────────
//
// Fires N concurrent transfers from the same wallet simultaneously.
// Asserts:
//   - Final balance = initialBalance - (successCount * transferAmount)   [no double-spend]
//   - Total DEBIT in ledger = successCount * transferAmount              [ledger accuracy]
//   - Total DEBIT == total CREDIT across all transfers                   [ledger balanced]

func TestIntegration_ConcurrencySafety_NoDoubleSpend(t *testing.T) {
	pool, teardown := setupIntegration(t)
	defer teardown()

	const (
		initialBalance = 1000.0
		transferAmount = 100.0
		goroutines     = 15 // more than balance can satisfy — only 10 should succeed
	)

	fromID := "integ-conc-from-" + uuid.New().String()[:8]
	toID := "integ-conc-to-" + uuid.New().String()[:8]
	seedWallet(t, pool, fromID, initialBalance)
	seedWallet(t, pool, toID, 0)

	svc := newSvc(pool)

	var (
		wg           sync.WaitGroup
		successCount atomic.Int32
		failCount    atomic.Int32
	)

	// All goroutines start at the same time.
	start := make(chan struct{})

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start // wait for the gun

			_, err := svc.CreateTransfer(context.Background(), &transfer.CreateTransferRequest{
				// No idempotency key — each goroutine is a distinct transfer.
				FromWalletID: fromID,
				ToWalletID:   toID,
				Amount:       transferAmount,
			})
			if err == nil {
				successCount.Add(1)
			} else {
				failCount.Add(1)
			}
		}(i)
	}

	close(start) // fire all goroutines simultaneously
	wg.Wait()

	t.Logf("concurrent transfers: %d succeeded, %d failed (insufficient balance or error)",
		successCount.Load(), failCount.Load())

	succeeded := int(successCount.Load())
	expectedBalance := initialBalance - float64(succeeded)*transferAmount

	// ── Assert 1: Final balance is correct (no double-spend) ─────────────────
	finalBalance := getBalance(t, pool, fromID)
	assert.Equal(t, expectedBalance, finalBalance,
		"balance mismatch — possible double-spend detected")

	// ── Assert 2: balance never went negative ─────────────────────────────────
	assert.GreaterOrEqual(t, finalBalance, 0.0, "balance must never be negative")

	// ── Assert 3: Ledger is balanced ─────────────────────────────────────────
	var totalDebited, totalCredited float64
	pool.QueryRow(context.Background(),
		`SELECT COALESCE(SUM(amount),0) FROM ledger_entries
		 WHERE wallet_id = $1 AND type = 'DEBIT'`, fromID,
	).Scan(&totalDebited) //nolint:errcheck
	pool.QueryRow(context.Background(),
		`SELECT COALESCE(SUM(amount),0) FROM ledger_entries
		 WHERE wallet_id = $1 AND type = 'CREDIT'`, toID,
	).Scan(&totalCredited) //nolint:errcheck

	assert.Equal(t, totalDebited, totalCredited,
		"ledger must be balanced: total DEBIT must equal total CREDIT")

	// ── Assert 4: Ledger amount matches actual balance change ─────────────────
	assert.Equal(t, float64(succeeded)*transferAmount, totalDebited,
		"ledger debit total must match number of successful transfers")

	t.Logf("final balance: %.2f | ledger debited: %.2f | ledger credited: %.2f",
		finalBalance, totalDebited, totalCredited)
}

// ─── Integration: concurrent idempotent requests ──────────────────────────────
//
// Same idempotency key fired from N goroutines simultaneously.
// Only ONE transfer must be executed; all callers receive the same transfer ID.

func TestIntegration_ConcurrencySafety_IdempotentRequests(t *testing.T) {
	pool, teardown := setupIntegration(t)
	defer teardown()

	fromID := "integ-idem-conc-from-" + uuid.New().String()[:8]
	toID := "integ-idem-conc-to-" + uuid.New().String()[:8]
	seedWallet(t, pool, fromID, 1000)
	seedWallet(t, pool, toID, 0)

	svc := newSvc(pool)

	const goroutines = 10
	const idempKey = "integ-idem-conc-key"

	var (
		wg          sync.WaitGroup
		mu          sync.Mutex
		transferIDs []string
	)

	start := make(chan struct{})

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start

			resp, err := svc.CreateTransfer(context.Background(), &transfer.CreateTransferRequest{
				IdempotencyKey: idempKey,
				FromWalletID:   fromID,
				ToWalletID:     toID,
				Amount:         100,
			})
			if err == nil {
				mu.Lock()
				transferIDs = append(transferIDs, resp.Transfer.ID)
				mu.Unlock()
			}
		}()
	}

	close(start)
	wg.Wait()

	// All successful responses must have the same transfer ID.
	require.NotEmpty(t, transferIDs, "at least one request should succeed")
	firstID := transferIDs[0]
	for _, id := range transferIDs {
		assert.Equal(t, firstID, id, "all concurrent idempotent requests must return the same transfer ID")
	}

	// Only ONE debit should have happened.
	assert.Equal(t, 900.0, getBalance(t, pool, fromID),
		"balance should reflect exactly one debit despite concurrent requests")

	// Only 2 ledger entries total.
	assert.Equal(t, 2, countLedgerEntries(t, pool, firstID),
		"must have exactly 2 ledger entries (1 DEBIT + 1 CREDIT)")

	// ── Wallet service integration: verify balance via service layer too ───────
	wSvc := walletService.NewWalletService(pool, walletRepo.NewWalletRepository())
	bal, err := wSvc.GetBalance(context.Background(), fromID)
	require.NoError(t, err)
	assert.Equal(t, 900.0, bal.Balance)
}
