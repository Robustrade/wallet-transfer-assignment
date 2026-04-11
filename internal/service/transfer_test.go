package service_test

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"wallet-transfer/internal/db"
	"wallet-transfer/internal/domain"
	"wallet-transfer/internal/repository"
	"wallet-transfer/internal/service"
)

// testEnv holds the service under test and a raw DB handle for seeding and
// asserting state without going through the service layer.
type testEnv struct {
	svc *service.TransferService
	db  *sql.DB
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	database, err := db.Open(":memory:?_foreign_keys=on")
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	walletRepo := repository.NewWalletRepository(database)
	transferRepo := repository.NewTransferRepository(database)
	ledgerRepo := repository.NewLedgerRepository()

	return &testEnv{
		svc: service.NewTransferService(database, walletRepo, transferRepo, ledgerRepo),
		db:  database,
	}
}

func (e *testEnv) seedWallet(t *testing.T, balance int64) string {
	t.Helper()
	id := uuid.New().String()
	now := time.Now().UTC()
	_, err := e.db.ExecContext(context.Background(),
		`INSERT INTO wallets (id, balance, created_at, updated_at) VALUES (?, ?, ?, ?)`,
		id, balance, now, now,
	)
	if err != nil {
		t.Fatalf("seed wallet: %v", err)
	}
	return id
}

func (e *testEnv) getBalance(t *testing.T, walletID string) int64 {
	t.Helper()
	var bal int64
	err := e.db.QueryRowContext(context.Background(),
		`SELECT balance FROM wallets WHERE id = ?`, walletID,
	).Scan(&bal)
	if err != nil {
		t.Fatalf("get balance for %s: %v", walletID, err)
	}
	return bal
}

func (e *testEnv) getLedgerEntries(t *testing.T, transferID string) []domain.LedgerEntry {
	t.Helper()
	rows, err := e.db.QueryContext(context.Background(),
		`SELECT id, transfer_id, wallet_id, entry_type, amount FROM ledger_entries WHERE transfer_id = ?`,
		transferID,
	)
	if err != nil {
		t.Fatalf("get ledger entries: %v", err)
	}
	defer rows.Close()

	var entries []domain.LedgerEntry
	for rows.Next() {
		var le domain.LedgerEntry
		if err := rows.Scan(&le.ID, &le.TransferID, &le.WalletID, &le.Type, &le.Amount); err != nil {
			t.Fatalf("scan ledger entry: %v", err)
		}
		entries = append(entries, le)
	}
	return entries
}

// --- Happy path ---

func TestCreateTransfer_Success(t *testing.T) {
	env := newTestEnv(t)
	from := env.seedWallet(t, 500)
	to := env.seedWallet(t, 100)

	transfer, isNew, err := env.svc.CreateTransfer(context.Background(), service.CreateTransferRequest{
		IdempotencyKey: "key-1",
		FromWalletID:   from,
		ToWalletID:     to,
		Amount:         200,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !isNew {
		t.Fatal("expected isNew=true for first transfer")
	}
	if transfer.Status != domain.StatusProcessed {
		t.Fatalf("expected PROCESSED, got %s", transfer.Status)
	}
	if env.getBalance(t, from) != 300 {
		t.Errorf("source balance: want 300, got %d", env.getBalance(t, from))
	}
	if env.getBalance(t, to) != 300 {
		t.Errorf("dest balance: want 300, got %d", env.getBalance(t, to))
	}
}

// --- Ledger correctness ---

func TestCreateTransfer_LedgerEntries(t *testing.T) {
	env := newTestEnv(t)
	from := env.seedWallet(t, 1000)
	to := env.seedWallet(t, 0)

	transfer, _, err := env.svc.CreateTransfer(context.Background(), service.CreateTransferRequest{
		IdempotencyKey: "ledger-key",
		FromWalletID:   from,
		ToWalletID:     to,
		Amount:         400,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	entries := env.getLedgerEntries(t, transfer.ID)
	if len(entries) != 2 {
		t.Fatalf("expected 2 ledger entries, got %d", len(entries))
	}

	var debit, credit *domain.LedgerEntry
	for i := range entries {
		switch entries[i].Type {
		case domain.EntryTypeDebit:
			debit = &entries[i]
		case domain.EntryTypeCredit:
			credit = &entries[i]
		}
	}
	if debit == nil || credit == nil {
		t.Fatal("missing DEBIT or CREDIT entry")
	}
	if debit.WalletID != from {
		t.Errorf("DEBIT wallet: want %s, got %s", from, debit.WalletID)
	}
	if credit.WalletID != to {
		t.Errorf("CREDIT wallet: want %s, got %s", to, credit.WalletID)
	}
	if debit.Amount != 400 || credit.Amount != 400 {
		t.Errorf("ledger amounts: debit=%d credit=%d, want 400 each", debit.Amount, credit.Amount)
	}
}

// --- Idempotency ---

func TestCreateTransfer_IdempotentReplay(t *testing.T) {
	env := newTestEnv(t)
	from := env.seedWallet(t, 500)
	to := env.seedWallet(t, 0)

	req := service.CreateTransferRequest{
		IdempotencyKey: "idem-1",
		FromWalletID:   from,
		ToWalletID:     to,
		Amount:         100,
	}

	first, isNew, err := env.svc.CreateTransfer(context.Background(), req)
	if err != nil || !isNew {
		t.Fatalf("first call: err=%v isNew=%v", err, isNew)
	}

	second, isNew, err := env.svc.CreateTransfer(context.Background(), req)
	if err != nil {
		t.Fatalf("second call unexpected error: %v", err)
	}
	if isNew {
		t.Fatal("expected isNew=false for replay")
	}
	if second.ID != first.ID {
		t.Errorf("replay returned different transfer ID: %s vs %s", second.ID, first.ID)
	}
	// Balance must not be debited twice.
	if bal := env.getBalance(t, from); bal != 400 {
		t.Errorf("source balance after replay: want 400, got %d", bal)
	}
}

func TestCreateTransfer_IdempotentReplay_ConcurrentRace(t *testing.T) {
	env := newTestEnv(t)
	from := env.seedWallet(t, 1000)
	to := env.seedWallet(t, 0)

	req := service.CreateTransferRequest{
		IdempotencyKey: "race-key",
		FromWalletID:   from,
		ToWalletID:     to,
		Amount:         100,
	}

	const goroutines = 10
	results := make([]*domain.Transfer, goroutines)
	errs := make([]error, goroutines)
	var wg sync.WaitGroup

	for i := range goroutines {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], _, errs[i] = env.svc.CreateTransfer(context.Background(), req)
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d: unexpected error: %v", i, err)
		}
	}

	// All goroutines must resolve to the same transfer ID.
	id := results[0].ID
	for i, r := range results {
		if r.ID != id {
			t.Errorf("goroutine %d returned different transfer ID %s, want %s", i, r.ID, id)
		}
	}

	// Balance debited exactly once.
	if bal := env.getBalance(t, from); bal != 900 {
		t.Errorf("source balance: want 900, got %d", bal)
	}
}

// --- Failure scenarios ---

func TestCreateTransfer_InsufficientFunds(t *testing.T) {
	env := newTestEnv(t)
	from := env.seedWallet(t, 50)
	to := env.seedWallet(t, 0)

	transfer, _, err := env.svc.CreateTransfer(context.Background(), service.CreateTransferRequest{
		IdempotencyKey: "insuf-1",
		FromWalletID:   from,
		ToWalletID:     to,
		Amount:         100,
	})

	if !errors.Is(err, domain.ErrInsufficientFunds) {
		t.Fatalf("expected ErrInsufficientFunds, got %v", err)
	}
	if transfer.Status != domain.StatusFailed {
		t.Errorf("expected FAILED status, got %s", transfer.Status)
	}
	// Source wallet balance unchanged.
	if bal := env.getBalance(t, from); bal != 50 {
		t.Errorf("source balance must be unchanged: want 50, got %d", bal)
	}
}

func TestCreateTransfer_InsufficientFunds_IdempotentReplay(t *testing.T) {
	env := newTestEnv(t)
	from := env.seedWallet(t, 50)
	to := env.seedWallet(t, 0)

	req := service.CreateTransferRequest{
		IdempotencyKey: "insuf-replay",
		FromWalletID:   from,
		ToWalletID:     to,
		Amount:         100,
	}

	first, _, _ := env.svc.CreateTransfer(context.Background(), req)
	second, isNew, _ := env.svc.CreateTransfer(context.Background(), req)

	if isNew {
		t.Fatal("replay of a failed transfer must return isNew=false")
	}
	if second.ID != first.ID {
		t.Error("replay must return the same transfer ID")
	}
	if second.Status != domain.StatusFailed {
		t.Errorf("replayed transfer status: want FAILED, got %s", second.Status)
	}
}

func TestCreateTransfer_WalletNotFound(t *testing.T) {
	env := newTestEnv(t)
	from := env.seedWallet(t, 500)

	_, _, err := env.svc.CreateTransfer(context.Background(), service.CreateTransferRequest{
		FromWalletID: from,
		ToWalletID:   "does-not-exist",
		Amount:       100,
	})
	if !errors.Is(err, domain.ErrWalletNotFound) {
		t.Fatalf("expected ErrWalletNotFound, got %v", err)
	}
}

func TestCreateTransfer_SameWallet(t *testing.T) {
	env := newTestEnv(t)
	w := env.seedWallet(t, 500)

	_, _, err := env.svc.CreateTransfer(context.Background(), service.CreateTransferRequest{
		FromWalletID: w,
		ToWalletID:   w,
		Amount:       100,
	})
	if !errors.Is(err, domain.ErrSameWallet) {
		t.Fatalf("expected ErrSameWallet, got %v", err)
	}
}

func TestCreateTransfer_ZeroAmount(t *testing.T) {
	env := newTestEnv(t)
	from := env.seedWallet(t, 500)
	to := env.seedWallet(t, 0)

	_, _, err := env.svc.CreateTransfer(context.Background(), service.CreateTransferRequest{
		FromWalletID: from,
		ToWalletID:   to,
		Amount:       0,
	})
	if !errors.Is(err, domain.ErrInvalidAmount) {
		t.Fatalf("expected ErrInvalidAmount, got %v", err)
	}
}

// --- Concurrency safety ---

func TestConcurrentTransfers_BalanceConsistency(t *testing.T) {
	env := newTestEnv(t)
	from := env.seedWallet(t, 1000)
	to := env.seedWallet(t, 0)

	const goroutines = 20
	const amount int64 = 10

	var wg sync.WaitGroup
	for i := range goroutines {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			//nolint:errcheck
			env.svc.CreateTransfer(context.Background(), service.CreateTransferRequest{
				IdempotencyKey: uuid.New().String(),
				FromWalletID:   from,
				ToWalletID:     to,
				Amount:         amount,
			})
		}(i)
	}
	wg.Wait()

	fromBal := env.getBalance(t, from)
	toBal := env.getBalance(t, to)

	if fromBal+toBal != 1000 {
		t.Errorf("ledger does not balance: from=%d to=%d total=%d, want 1000", fromBal, toBal, fromBal+toBal)
	}
	want := int64(1000 - goroutines*amount)
	if fromBal != want {
		t.Errorf("source balance: want %d, got %d", want, fromBal)
	}
}
