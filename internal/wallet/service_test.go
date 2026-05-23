package wallet_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/joho/godotenv"
	_ "github.com/lib/pq"

	"shindechandrakant/internal/api/dtos"
	"shindechandrakant/internal/wallet"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	_ = godotenv.Load("../../.env")

	host := os.Getenv("POSTGRES_HOST")
	if host == "" {
		t.Skip("POSTGRES_HOST not set — skipping integration tests")
	}

	dsn := fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		os.Getenv("POSTGRES_HOST"),
		os.Getenv("POSTGRES_PORT"),
		os.Getenv("POSTGRES_USER"),
		os.Getenv("POSTGRES_PASSWORD"),
		os.Getenv("POSTGRES_DB"),
	)

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("failed to open test DB: %v", err)
	}
	if err := db.Ping(); err != nil {
		t.Skipf("cannot reach test DB: %v", err)
	}

	t.Cleanup(func() { db.Close() })
	return db
}

// createTestWallet inserts a wallet and registers cleanup that removes it and
// all related rows after the test, respecting FK constraints.
func createTestWallet(t *testing.T, db *sql.DB, balance int64) string {
	t.Helper()
	id := "tw_" + uuid.New().String()[:8]

	_, err := db.Exec(
		`INSERT INTO wallets (wallet_id, balance) VALUES ($1, $2)`,
		id, balance,
	)
	if err != nil {
		t.Fatalf("createTestWallet: %v", err)
	}

	t.Cleanup(func() {
		db.Exec(`DELETE FROM ledger_entries WHERE wallet_id = $1`, id)
		db.Exec(`DELETE FROM transfers WHERE from_wallet_id = $1 OR to_wallet_id = $1`, id)
		db.Exec(`DELETE FROM wallets WHERE wallet_id = $1`, id)
	})
	return id
}

func getBalance(t *testing.T, db *sql.DB, walletID string) int64 {
	t.Helper()
	var b int64
	if err := db.QueryRow(`SELECT balance FROM wallets WHERE wallet_id = $1`, walletID).Scan(&b); err != nil {
		t.Fatalf("getBalance(%s): %v", walletID, err)
	}
	return b
}

func countLedgerEntries(t *testing.T, db *sql.DB, transactionID string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM ledger_entries WHERE transaction_id = $1`, transactionID).Scan(&n); err != nil {
		t.Fatalf("countLedgerEntries(%s): %v", transactionID, err)
	}
	return n
}

func newService(db *sql.DB) wallet.Service {
	return wallet.NewWalletService(wallet.NewWalletRepository(db))
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestTransfer_Success(t *testing.T) {
	db := setupTestDB(t)
	svc := newService(db)

	fromID := createTestWallet(t, db, 10000) // 100.00
	toID := createTestWallet(t, db, 5000)    //  50.00

	req := dtos.TransferRequest{
		IdempotencyKey: uuid.New().String(),
		FromWalletId:   fromID,
		ToWalletId:     toID,
		Amount:         20.00,
	}

	transfer, err := svc.Transfer(context.Background(), req)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if transfer.TransactionId == "" {
		t.Error("expected non-empty transaction ID")
	}
	if transfer.State != wallet.StateProcessed {
		t.Errorf("expected state PROCESSED, got %s", transfer.State)
	}
	if transfer.Amount != 2000 {
		t.Errorf("expected amount 2000 minor units, got %d", transfer.Amount)
	}

	// Balances must reflect the debit/credit
	if got := getBalance(t, db, fromID); got != 8000 {
		t.Errorf("from wallet: expected 8000, got %d", got)
	}
	if got := getBalance(t, db, toID); got != 7000 {
		t.Errorf("to wallet: expected 7000, got %d", got)
	}

	// Exactly two ledger entries
	if n := countLedgerEntries(t, db, transfer.TransactionId); n != 2 {
		t.Errorf("expected 2 ledger entries, got %d", n)
	}
}

func TestTransfer_Idempotency_DuplicateRequestReturnsSameTransfer(t *testing.T) {
	db := setupTestDB(t)
	svc := newService(db)

	fromID := createTestWallet(t, db, 10000)
	toID := createTestWallet(t, db, 5000)

	req := dtos.TransferRequest{
		IdempotencyKey: uuid.New().String(),
		FromWalletId:   fromID,
		ToWalletId:     toID,
		Amount:         10.00,
	}

	first, err := svc.Transfer(context.Background(), req)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}

	second, err := svc.Transfer(context.Background(), req)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}

	// Must return the same transfer
	if first.TransactionId != second.TransactionId {
		t.Errorf("idempotency broken: got different transaction IDs (%s vs %s)",
			first.TransactionId, second.TransactionId)
	}

	// Balance must only be debited once
	if got := getBalance(t, db, fromID); got != 9000 {
		t.Errorf("double debit detected: expected 9000, got %d", got)
	}

	// Still only two ledger entries, not four
	if n := countLedgerEntries(t, db, first.TransactionId); n != 2 {
		t.Errorf("expected 2 ledger entries after duplicate call, got %d", n)
	}
}

func TestTransfer_InsufficientFunds(t *testing.T) {
	db := setupTestDB(t)
	svc := newService(db)

	fromID := createTestWallet(t, db, 500) // 5.00
	toID := createTestWallet(t, db, 0)

	_, err := svc.Transfer(context.Background(), dtos.TransferRequest{
		IdempotencyKey: uuid.New().String(),
		FromWalletId:   fromID,
		ToWalletId:     toID,
		Amount:         10.00, // more than available
	})

	if err == nil {
		t.Fatal("expected error for insufficient funds, got nil")
	}
	if err.Error() != wallet.ErrInsufficientFunds.Error() {
		t.Errorf("expected ErrInsufficientFunds, got: %v", err)
	}

	// Balance must be unchanged
	if got := getBalance(t, db, fromID); got != 500 {
		t.Errorf("balance changed on failed transfer: expected 500, got %d", got)
	}
}

func TestTransfer_SameWallet(t *testing.T) {
	db := setupTestDB(t)
	svc := newService(db)

	walletID := createTestWallet(t, db, 10000)

	_, err := svc.Transfer(context.Background(), dtos.TransferRequest{
		IdempotencyKey: uuid.New().String(),
		FromWalletId:   walletID,
		ToWalletId:     walletID,
		Amount:         10.00,
	})

	if err == nil {
		t.Fatal("expected error for same-wallet transfer, got nil")
	}
	if err.Error() != wallet.ErrSameWallet.Error() {
		t.Errorf("expected ErrSameWallet, got: %v", err)
	}
}

func TestTransfer_WalletNotFound(t *testing.T) {
	db := setupTestDB(t)
	svc := newService(db)

	fromID := createTestWallet(t, db, 10000)

	_, err := svc.Transfer(context.Background(), dtos.TransferRequest{
		IdempotencyKey: uuid.New().String(),
		FromWalletId:   fromID,
		ToWalletId:     "wallet_does_not_exist",
		Amount:         10.00,
	})

	if err == nil {
		t.Fatal("expected error for non-existent wallet, got nil")
	}
	if err.Error() != wallet.ErrWalletNotFound.Error() {
		t.Errorf("expected ErrWalletNotFound, got: %v", err)
	}
}

func TestTransfer_InactiveWallet(t *testing.T) {
	db := setupTestDB(t)
	svc := newService(db)

	fromID := createTestWallet(t, db, 10000)
	toID := createTestWallet(t, db, 0)

	// Mark destination wallet inactive
	if _, err := db.Exec(`UPDATE wallets SET status = 'INACTIVE' WHERE wallet_id = $1`, toID); err != nil {
		t.Fatalf("marking wallet inactive: %v", err)
	}

	_, err := svc.Transfer(context.Background(), dtos.TransferRequest{
		IdempotencyKey: uuid.New().String(),
		FromWalletId:   fromID,
		ToWalletId:     toID,
		Amount:         10.00,
	})

	if err == nil {
		t.Fatal("expected error for inactive wallet, got nil")
	}
	if err.Error() != wallet.ErrWalletInactive.Error() {
		t.Errorf("expected ErrWalletInactive, got: %v", err)
	}
}

func TestTransfer_LedgerEntries_DebitAndCreditAreCorrect(t *testing.T) {
	db := setupTestDB(t)
	svc := newService(db)

	fromID := createTestWallet(t, db, 10000)
	toID := createTestWallet(t, db, 2000)

	transfer, err := svc.Transfer(context.Background(), dtos.TransferRequest{
		IdempotencyKey: uuid.New().String(),
		FromWalletId:   fromID,
		ToWalletId:     toID,
		Amount:         30.00,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	rows, err := db.Query(`
		SELECT wallet_id, entry, amount, balance_before, balance_after
		FROM ledger_entries
		WHERE transaction_id = $1
		ORDER BY entry
	`, transfer.TransactionId)
	if err != nil {
		t.Fatalf("querying ledger entries: %v", err)
	}
	defer rows.Close()

	type entry struct {
		walletID      string
		entryType     string
		amount        int64
		balanceBefore int64
		balanceAfter  int64
	}

	var entries []entry
	for rows.Next() {
		var e entry
		if err := rows.Scan(&e.walletID, &e.entryType, &e.amount, &e.balanceBefore, &e.balanceAfter); err != nil {
			t.Fatalf("scanning ledger entry: %v", err)
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterating ledger entries: %v", err)
	}

	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}

	// Find debit and credit entries
	var debit, credit entry
	for _, e := range entries {
		if e.entryType == "CREDIT" {
			credit = e
		} else {
			debit = e
		}
	}

	if debit.walletID != fromID {
		t.Errorf("DEBIT should be on sender, got walletID=%s", debit.walletID)
	}
	if credit.walletID != toID {
		t.Errorf("CREDIT should be on receiver, got walletID=%s", credit.walletID)
	}
	if debit.amount != 3000 {
		t.Errorf("debit amount: expected 3000, got %d", debit.amount)
	}
	if credit.amount != 3000 {
		t.Errorf("credit amount: expected 3000, got %d", credit.amount)
	}
	if debit.balanceBefore != 10000 || debit.balanceAfter != 7000 {
		t.Errorf("debit balance trail wrong: before=%d after=%d", debit.balanceBefore, debit.balanceAfter)
	}
	if credit.balanceBefore != 2000 || credit.balanceAfter != 5000 {
		t.Errorf("credit balance trail wrong: before=%d after=%d", credit.balanceBefore, credit.balanceAfter)
	}
}

// TestTransfer_Concurrent launches N goroutines all transferring from the same
// wallet simultaneously. Final balance must be exactly correct — no double spend.
func TestTransfer_Concurrent_NoDoubleSpend(t *testing.T) {
	db := setupTestDB(t)
	svc := newService(db)

	const (
		goroutines      = 10
		amountEach      = 10.00                    // 1000 minor units
		startingBalance = int64(goroutines * 1000) // 10000 — exactly enough
	)

	fromID := createTestWallet(t, db, startingBalance)
	toID := createTestWallet(t, db, 0)

	var wg sync.WaitGroup
	errs := make([]error, goroutines)

	for i := range goroutines {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = svc.Transfer(context.Background(), dtos.TransferRequest{
				IdempotencyKey: fmt.Sprintf("concurrent-key-%d", i),
				FromWalletId:   fromID,
				ToWalletId:     toID,
				Amount:         amountEach,
			})
		}(i)
	}

	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d: unexpected error: %v", i, err)
		}
	}

	if got := getBalance(t, db, fromID); got != 0 {
		t.Errorf("from wallet: expected 0 after all transfers, got %d", got)
	}
	if got := getBalance(t, db, toID); got != startingBalance {
		t.Errorf("to wallet: expected %d, got %d", startingBalance, got)
	}
}
