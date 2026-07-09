package service_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	_ "github.com/mattn/go-sqlite3"
	"github.com/vardaanmittal09/wallet-transfer-assignment/internal/domain"
	"github.com/vardaanmittal09/wallet-transfer-assignment/internal/repository/sqlite"
	"github.com/vardaanmittal09/wallet-transfer-assignment/internal/service"
)

func TestCreateTransferProcessesLedgerAndBalances(t *testing.T) {
	ctx := context.Background()
	db, svc := newTestService(t, map[string]int64{"wallet_1": 500, "wallet_2": 25})

	result, err := svc.CreateTransfer(ctx, service.CreateTransferInput{
		IdempotencyKey: "key-1",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         125,
	})
	if err != nil {
		t.Fatalf("create transfer: %v", err)
	}

	if result.State != domain.TransferProcessed {
		t.Fatalf("state = %s, want %s", result.State, domain.TransferProcessed)
	}
	if len(result.LedgerEntries) != 2 {
		t.Fatalf("ledger entries = %d, want 2", len(result.LedgerEntries))
	}
	assertWalletBalance(t, db, "wallet_1", 375)
	assertWalletBalance(t, db, "wallet_2", 150)
	assertBalancedLedger(t, result.LedgerEntries, 125)
}

func TestCreateTransferIsIdempotent(t *testing.T) {
	ctx := context.Background()
	db, svc := newTestService(t, map[string]int64{"wallet_1": 500, "wallet_2": 25})
	input := service.CreateTransferInput{
		IdempotencyKey: "same-key",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         125,
	}

	first, err := svc.CreateTransfer(ctx, input)
	if err != nil {
		t.Fatalf("first create: %v", err)
	}
	second, err := svc.CreateTransfer(ctx, input)
	if err != nil {
		t.Fatalf("second create: %v", err)
	}

	if !second.Replay {
		t.Fatal("second result should be marked as replay")
	}
	if first.ID != second.ID {
		t.Fatalf("transfer ids differ: %s != %s", first.ID, second.ID)
	}
	assertWalletBalance(t, db, "wallet_1", 375)
	assertWalletBalance(t, db, "wallet_2", 150)
	assertTableCount(t, db, "transfers", 1)
	assertTableCount(t, db, "ledger_entries", 2)
}

func TestIdempotencyKeyRejectsDifferentPayload(t *testing.T) {
	ctx := context.Background()
	_, svc := newTestService(t, map[string]int64{"wallet_1": 500, "wallet_2": 25, "wallet_3": 10})

	_, err := svc.CreateTransfer(ctx, service.CreateTransferInput{
		IdempotencyKey: "same-key",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         125,
	})
	if err != nil {
		t.Fatalf("first create: %v", err)
	}

	_, err = svc.CreateTransfer(ctx, service.CreateTransferInput{
		IdempotencyKey: "same-key",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_3",
		Amount:         125,
	})
	if !errors.Is(err, domain.ErrIdempotencyConflict) {
		t.Fatalf("err = %v, want %v", err, domain.ErrIdempotencyConflict)
	}
}

func TestInsufficientFundsCreatesFailedTransferWithoutLedgerSideEffects(t *testing.T) {
	ctx := context.Background()
	db, svc := newTestService(t, map[string]int64{"wallet_1": 100, "wallet_2": 25})

	result, err := svc.CreateTransfer(ctx, service.CreateTransferInput{
		IdempotencyKey: "too-large",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         125,
	})
	if err != nil {
		t.Fatalf("create transfer: %v", err)
	}

	if result.State != domain.TransferFailed {
		t.Fatalf("state = %s, want %s", result.State, domain.TransferFailed)
	}
	if result.FailureReason != domain.ErrInsufficientFunds.Error() {
		t.Fatalf("failure reason = %q", result.FailureReason)
	}
	assertWalletBalance(t, db, "wallet_1", 100)
	assertWalletBalance(t, db, "wallet_2", 25)
	assertTableCount(t, db, "ledger_entries", 0)
}

func TestConcurrentTransfersDoNotOverspend(t *testing.T) {
	ctx := context.Background()
	db, svc := newTestService(t, map[string]int64{"wallet_1": 1000, "wallet_2": 0})

	const attempts = 20
	const amount = 75

	var wg sync.WaitGroup
	results := make(chan service.TransferResult, attempts)
	errs := make(chan error, attempts)
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			result, err := svc.CreateTransfer(ctx, service.CreateTransferInput{
				IdempotencyKey: fmt.Sprintf("concurrent-%02d", i),
				FromWalletID:   "wallet_1",
				ToWalletID:     "wallet_2",
				Amount:         amount,
			})
			if err != nil {
				errs <- err
				return
			}
			results <- result
		}(i)
	}
	wg.Wait()
	close(results)
	close(errs)

	for err := range errs {
		t.Fatalf("concurrent create returned error: %v", err)
	}

	var processed int
	var failed int
	for result := range results {
		switch result.State {
		case domain.TransferProcessed:
			processed++
		case domain.TransferFailed:
			failed++
		default:
			t.Fatalf("unexpected state: %s", result.State)
		}
	}

	if processed != 13 || failed != 7 {
		t.Fatalf("processed=%d failed=%d, want processed=13 failed=7", processed, failed)
	}
	assertWalletBalance(t, db, "wallet_1", 25)
	assertWalletBalance(t, db, "wallet_2", 975)
	assertTableCount(t, db, "ledger_entries", processed*2)
}

func newTestService(t *testing.T, wallets map[string]int64) (*sql.DB, *service.TransferService) {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "wallets.db")
	db, err := sql.Open("sqlite3", sqlite.DSN(dbPath))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	store := sqlite.NewStore(db)
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := store.SeedWallets(context.Background(), wallets); err != nil {
		t.Fatalf("seed wallets: %v", err)
	}
	return db, service.NewTransferService(store)
}

func assertWalletBalance(t *testing.T, db *sql.DB, walletID string, want int64) {
	t.Helper()

	var got int64
	if err := db.QueryRow(`SELECT balance FROM wallets WHERE id = ?`, walletID).Scan(&got); err != nil {
		t.Fatalf("get wallet balance: %v", err)
	}
	if got != want {
		t.Fatalf("%s balance = %d, want %d", walletID, got, want)
	}
}

func assertTableCount(t *testing.T, db *sql.DB, table string, want int) {
	t.Helper()

	var got int
	if err := db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&got); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	if got != want {
		t.Fatalf("%s count = %d, want %d", table, got, want)
	}
}

func assertBalancedLedger(t *testing.T, entries []domain.LedgerEntry, amount int64) {
	t.Helper()

	var debit int64
	var credit int64
	for _, entry := range entries {
		switch entry.Type {
		case domain.LedgerDebit:
			debit += entry.Amount
		case domain.LedgerCredit:
			credit += entry.Amount
		default:
			t.Fatalf("unexpected ledger entry type: %s", entry.Type)
		}
	}
	if debit != amount || credit != amount {
		t.Fatalf("debit=%d credit=%d, want %d each", debit, credit, amount)
	}
}
