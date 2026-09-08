package service_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/gaurav2204bits-collab/wallet-transfer-assignment/internal/domain"
	"github.com/gaurav2204bits-collab/wallet-transfer-assignment/internal/platform/id"
	"github.com/gaurav2204bits-collab/wallet-transfer-assignment/internal/repository/memory"
	"github.com/gaurav2204bits-collab/wallet-transfer-assignment/internal/service"
)

func newSVC(t *testing.T) (*service.TransferService, *memory.Store) {
	t.Helper()
	store := memory.New()
	store.SeedWallet("wallet_1", 1_000, "USD")
	store.SeedWallet("wallet_2", 0, "USD")
	repos := service.Repositories{
		Wallets:     store,
		Transfers:   store,
		Ledger:      store,
		Idempotency: store,
	}
	svc := service.NewTransferService(repos, store, id.NewGenerator("txn", 8), nil)
	return svc, store
}

func cmd(key string, amount int64) service.CreateTransferCommand {
	return service.CreateTransferCommand{
		IdempotencyKey: key,
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         amount,
	}
}

func TestCreateTransfer_HappyPath(t *testing.T) {
	t.Parallel()
	svc, store := newSVC(t)

	res, err := svc.CreateTransfer(context.Background(), cmd("k1", 250))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Replayed {
		t.Fatal("first call should not be a replay")
	}
	if res.Transfer.State != domain.TransferProcessed {
		t.Fatalf("state = %s, want PROCESSED", res.Transfer.State)
	}

	from, _ := store.Get(context.Background(), "wallet_1")
	to, _ := store.Get(context.Background(), "wallet_2")
	if from.Balance.Amount != 750 || to.Balance.Amount != 250 {
		t.Fatalf("balances = %d / %d, want 750 / 250", from.Balance.Amount, to.Balance.Amount)
	}
	if store.LedgerLen() != 2 {
		t.Fatalf("ledger entries = %d, want 2", store.LedgerLen())
	}

	debitSum, _ := store.SumByWallet(context.Background(), "wallet_1")
	creditSum, _ := store.SumByWallet(context.Background(), "wallet_2")
	if debitSum.Amount+creditSum.Amount != 0 {
		t.Fatalf("ledger does not balance: %d + %d", debitSum.Amount, creditSum.Amount)
	}
}

func TestCreateTransfer_IdempotentReplay(t *testing.T) {
	t.Parallel()
	svc, store := newSVC(t)
	ctx := context.Background()

	first, err := svc.CreateTransfer(ctx, cmd("dup", 300))
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	second, err := svc.CreateTransfer(ctx, cmd("dup", 300))
	if err != nil {
		t.Fatalf("second call: %v", err)
	}

	if !second.Replayed {
		t.Fatal("second call should be a replay")
	}
	if first.Transfer.ID != second.Transfer.ID {
		t.Fatalf("replay returned a different transfer: %s vs %s", first.Transfer.ID, second.Transfer.ID)
	}
	from, _ := store.Get(ctx, "wallet_1")
	if from.Balance.Amount != 700 {
		t.Fatalf("balance moved twice: %d, want 700", from.Balance.Amount)
	}
	if store.LedgerLen() != 2 {
		t.Fatalf("ledger entries = %d, want 2 (no duplicate side effects)", store.LedgerLen())
	}
}

func TestCreateTransfer_IdempotencyKeyConflict(t *testing.T) {
	t.Parallel()
	svc, _ := newSVC(t)
	ctx := context.Background()

	if _, err := svc.CreateTransfer(ctx, cmd("reused", 100)); err != nil {
		t.Fatalf("first call: %v", err)
	}
	_, err := svc.CreateTransfer(ctx, cmd("reused", 999)) // same key, different amount
	if !errors.Is(err, domain.ErrIdempotencyKeyConflict) {
		t.Fatalf("got %v, want ErrIdempotencyKeyConflict", err)
	}
}

func TestCreateTransfer_InsufficientFunds(t *testing.T) {
	t.Parallel()
	svc, store := newSVC(t)
	ctx := context.Background()

	res, err := svc.CreateTransfer(ctx, cmd("poor", 5_000))
	if err != nil {
		t.Fatalf("unexpected error (insufficient funds is a committed FAILED transfer): %v", err)
	}
	if res.Transfer.State != domain.TransferFailed {
		t.Fatalf("state = %s, want FAILED", res.Transfer.State)
	}
	from, _ := store.Get(ctx, "wallet_1")
	if from.Balance.Amount != 1_000 {
		t.Fatalf("balance changed on failed transfer: %d", from.Balance.Amount)
	}
	if store.LedgerLen() != 0 {
		t.Fatalf("ledger entries written for failed transfer: %d", store.LedgerLen())
	}

	// Retry replays the same failure, no execution.
	replay, err := svc.CreateTransfer(ctx, cmd("poor", 5_000))
	if err != nil {
		t.Fatalf("replay of failed transfer: %v", err)
	}
	if !replay.Replayed || replay.Transfer.ID != res.Transfer.ID {
		t.Fatalf("failed transfer did not replay cleanly")
	}
}

func TestCreateTransfer_WalletNotFound(t *testing.T) {
	t.Parallel()
	svc, _ := newSVC(t)
	c := cmd("missing", 100)
	c.ToWalletID = "wallet_absent"
	_, err := svc.CreateTransfer(context.Background(), c)
	if !errors.Is(err, domain.ErrWalletNotFound) {
		t.Fatalf("got %v, want ErrWalletNotFound", err)
	}
}

func TestCreateTransfer_Validation(t *testing.T) {
	t.Parallel()
	svc, _ := newSVC(t)

	cases := map[string]service.CreateTransferCommand{
		"self transfer": {IdempotencyKey: "k", FromWalletID: "wallet_1", ToWalletID: "wallet_1", Amount: 1},
		"zero amount":   {IdempotencyKey: "k", FromWalletID: "wallet_1", ToWalletID: "wallet_2", Amount: 0},
		"negative":      {IdempotencyKey: "k", FromWalletID: "wallet_1", ToWalletID: "wallet_2", Amount: -1},
		"missing key":   {FromWalletID: "wallet_1", ToWalletID: "wallet_2", Amount: 1},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := svc.CreateTransfer(context.Background(), c)
			if !errors.Is(err, domain.ErrValidation) {
				t.Fatalf("got %v, want ErrValidation", err)
			}
		})
	}
}

// TestCreateTransfer_ConcurrentDebits is the double-spend guard: many goroutines
// race to debit one wallet that can only fund a subset of them.
func TestCreateTransfer_ConcurrentDebits(t *testing.T) {
	t.Parallel()

	store := memory.New()
	store.SeedWallet("wallet_1", 1_000, "USD")
	store.SeedWallet("wallet_2", 0, "USD")
	repos := service.Repositories{Wallets: store, Transfers: store, Ledger: store, Idempotency: store}
	svc := service.NewTransferService(repos, store, id.NewGenerator("txn", 8), nil)

	const workers = 50
	const amount = 100 // only 10 can succeed

	var processed, failed atomic.Int64
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func(i int) {
			defer wg.Done()
			res, err := svc.CreateTransfer(context.Background(), service.CreateTransferCommand{
				IdempotencyKey: "concurrent-" + id.NewGenerator("k", 6).NewID(),
				FromWalletID:   "wallet_1",
				ToWalletID:     "wallet_2",
				Amount:         amount,
			})
			if err != nil {
				t.Errorf("worker %d: %v", i, err)
				return
			}
			switch res.Transfer.State {
			case domain.TransferProcessed:
				processed.Add(1)
			case domain.TransferFailed:
				failed.Add(1)
			}
		}(i)
	}
	wg.Wait()

	if processed.Load() != 10 {
		t.Fatalf("processed = %d, want exactly 10", processed.Load())
	}
	if failed.Load() != workers-10 {
		t.Fatalf("failed = %d, want %d", failed.Load(), workers-10)
	}

	from, _ := store.Get(context.Background(), "wallet_1")
	to, _ := store.Get(context.Background(), "wallet_2")
	if from.Balance.Amount != 0 || to.Balance.Amount != 1_000 {
		t.Fatalf("final balances = %d / %d, want 0 / 1000", from.Balance.Amount, to.Balance.Amount)
	}
	if store.LedgerLen() != 20 {
		t.Fatalf("ledger entries = %d, want 20", store.LedgerLen())
	}
}
