package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/avinilcodes/wallet-transfer-assignment/internal/domain"
	"github.com/avinilcodes/wallet-transfer-assignment/internal/repository/memory"
)

func testStore(t *testing.T) *memory.Store {
	t.Helper()
	store := memory.NewStore()
	store.SeedWallet(&domain.Wallet{ID: "wallet_1", UserID: "user_1", Balance: 500, Currency: "USD"})
	store.SeedWallet(&domain.Wallet{ID: "wallet_2", UserID: "user_2", Balance: 200, Currency: "USD"})
	return store
}

func testInput(key string, amount int64) CreateTransferInput {
	return CreateTransferInput{
		IdempotencyKey: key,
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         amount,
	}
}

func TestCreateTransfer_executesSuccessfully(t *testing.T) {
	store := testStore(t)
	svc := NewTransferService(store)

	result, err := svc.CreateTransfer(context.Background(), testInput("idem-1", 100))
	if err != nil {
		t.Fatalf("CreateTransfer: %v", err)
	}
	if result.State != domain.TransferStateProcessed {
		t.Fatalf("state = %s, want PROCESSED", result.State)
	}
	if result.Amount != 100 || result.Currency != "USD" {
		t.Fatalf("unexpected result: %+v", result)
	}

	w1, _ := store.Wallet("wallet_1")
	w2, _ := store.Wallet("wallet_2")
	if w1.Balance != 400 {
		t.Fatalf("source balance = %d, want 400", w1.Balance)
	}
	if w2.Balance != 300 {
		t.Fatalf("target balance = %d, want 300", w2.Balance)
	}

	tr, ok := store.Transfer(result.TransferID)
	if !ok || tr.State != domain.TransferStateProcessed {
		t.Fatalf("transfer not processed: %+v", tr)
	}
}

func TestCreateTransfer_ledgerIsDoubleEntry(t *testing.T) {
	store := testStore(t)
	svc := NewTransferService(store)

	result, err := svc.CreateTransfer(context.Background(), testInput("idem-ledger", 75))
	if err != nil {
		t.Fatalf("CreateTransfer: %v", err)
	}

	entries, err := store.Ledger().ListByTransferID(context.Background(), result.TransferID)
	if err != nil {
		t.Fatalf("ListByTransferID: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("ledger entries = %d, want 2", len(entries))
	}

	var debit, credit *domain.LedgerEntry
	for i := range entries {
		switch entries[i].Type {
		case domain.LedgerEntryTypeDebit:
			debit = &entries[i]
		case domain.LedgerEntryTypeCredit:
			credit = &entries[i]
		}
	}
	if debit == nil || credit == nil {
		t.Fatal("expected one DEBIT and one CREDIT entry")
	}
	if debit.WalletID != "wallet_1" || credit.WalletID != "wallet_2" {
		t.Fatalf("unexpected wallets: debit=%s credit=%s", debit.WalletID, credit.WalletID)
	}
	if debit.Amount != 75 || credit.Amount != 75 {
		t.Fatalf("amounts not balanced: debit=%d credit=%d", debit.Amount, credit.Amount)
	}

	w1, _ := store.Wallet("wallet_1")
	w2, _ := store.Wallet("wallet_2")
	totalBefore := int64(500 + 200)
	totalAfter := w1.Balance + w2.Balance
	if totalAfter != totalBefore {
		t.Fatalf("money not conserved: before=%d after=%d", totalBefore, totalAfter)
	}
}

func TestCreateTransfer_idempotencyReplay(t *testing.T) {
	store := testStore(t)
	svc := NewTransferService(store)
	input := testInput("idem-replay", 50)

	first, err := svc.CreateTransfer(context.Background(), input)
	if err != nil {
		t.Fatalf("first CreateTransfer: %v", err)
	}

	second, err := svc.CreateTransfer(context.Background(), input)
	if err != nil {
		t.Fatalf("replay CreateTransfer: %v", err)
	}
	if second.TransferID != first.TransferID {
		t.Fatalf("transfer id changed: %s vs %s", first.TransferID, second.TransferID)
	}

	w1, _ := store.Wallet("wallet_1")
	if w1.Balance != 450 {
		t.Fatalf("balance debited twice: %d", w1.Balance)
	}

	if len(store.LedgerEntries()) != 2 {
		t.Fatalf("ledger rows = %d, want 2", len(store.LedgerEntries()))
	}
}

func TestCreateTransfer_idempotencyConflict(t *testing.T) {
	store := testStore(t)
	svc := NewTransferService(store)

	_, err := svc.CreateTransfer(context.Background(), testInput("idem-conflict", 25))
	if err != nil {
		t.Fatalf("first CreateTransfer: %v", err)
	}

	conflict := testInput("idem-conflict", 99)
	_, err = svc.CreateTransfer(context.Background(), conflict)
	if !errors.Is(err, domain.ErrIdempotencyConflict) {
		t.Fatalf("got %v, want ErrIdempotencyConflict", err)
	}
}

func TestCreateTransfer_insufficientFunds(t *testing.T) {
	store := testStore(t)
	svc := NewTransferService(store)

	_, err := svc.CreateTransfer(context.Background(), testInput("idem-fail-funds", 1000))
	if !errors.Is(err, domain.ErrInsufficientFunds) {
		t.Fatalf("got %v, want ErrInsufficientFunds", err)
	}

	w1, _ := store.Wallet("wallet_1")
	if w1.Balance != 500 {
		t.Fatalf("balance changed on failed transfer: %d", w1.Balance)
	}
	if len(store.LedgerEntries()) != 0 {
		t.Fatalf("ledger should be empty, got %d entries", len(store.LedgerEntries()))
	}
}

func TestCreateTransfer_currencyMismatch(t *testing.T) {
	store := memory.NewStore()
	store.SeedWallet(&domain.Wallet{ID: "wallet_1", UserID: "user_1", Balance: 100, Currency: "USD"})
	store.SeedWallet(&domain.Wallet{ID: "wallet_eur", UserID: "user_2", Balance: 100, Currency: "EUR"})
	svc := NewTransferService(store)

	_, err := svc.CreateTransfer(context.Background(), CreateTransferInput{
		IdempotencyKey: "idem-currency",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_eur",
		Amount:         10,
	})
	if !errors.Is(err, domain.ErrInvalidTransfer) {
		t.Fatalf("got %v, want ErrInvalidTransfer", err)
	}
}

func TestCreateTransfer_walletNotFound(t *testing.T) {
	store := testStore(t)
	svc := NewTransferService(store)

	_, err := svc.CreateTransfer(context.Background(), CreateTransferInput{
		IdempotencyKey: "idem-missing",
		FromWalletID:   "wallet_1",
		ToWalletID:     "missing_wallet",
		Amount:         10,
	})
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("got %v, want ErrNotFound", err)
	}
}

func TestCreateTransfer_sameWalletRejected(t *testing.T) {
	store := testStore(t)
	svc := NewTransferService(store)

	_, err := svc.CreateTransfer(context.Background(), CreateTransferInput{
		IdempotencyKey: "idem-same",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_1",
		Amount:         10,
	})
	if !errors.Is(err, domain.ErrSameWallet) {
		t.Fatalf("got %v, want ErrSameWallet", err)
	}
}

func TestCreateTransfer_concurrentDebitsDoNotOverdraw(t *testing.T) {
	store := memory.NewStore()
	store.SeedWallet(&domain.Wallet{ID: "wallet_1", UserID: "user_1", Balance: 100, Currency: "USD"})
	store.SeedWallet(&domain.Wallet{ID: "wallet_2", UserID: "user_2", Balance: 0, Currency: "USD"})
	svc := NewTransferService(store)

	const (
		workers   = 10
		amount    = int64(40)
		maxWins   = 2 // 2 * 40 fits in 100, third must fail
	)

	var successCount atomic.Int32
	var wg sync.WaitGroup
	wg.Add(workers)

	for i := 0; i < workers; i++ {
		i := i
		go func() {
			defer wg.Done()
			_, err := svc.CreateTransfer(context.Background(), CreateTransferInput{
				IdempotencyKey: fmt.Sprintf("concurrent-%d", i),
				FromWalletID:   "wallet_1",
				ToWalletID:     "wallet_2",
				Amount:         amount,
			})
			if err == nil {
				successCount.Add(1)
			}
		}()
	}
	wg.Wait()

	if got := int(successCount.Load()); got != maxWins {
		t.Fatalf("successful transfers = %d, want %d", got, maxWins)
	}

	w1, _ := store.Wallet("wallet_1")
	if w1.Balance != 20 {
		t.Fatalf("final source balance = %d, want 20", w1.Balance)
	}
}
