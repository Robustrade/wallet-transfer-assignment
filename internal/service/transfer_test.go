package service_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/ankurnitt1311/wallet-transfer-assignment/internal/domain"
	"github.com/ankurnitt1311/wallet-transfer-assignment/internal/repository"
	"github.com/ankurnitt1311/wallet-transfer-assignment/internal/service"
)

func newService(t *testing.T, concurrent bool) (*service.Service, *repository.Store) {
	t.Helper()
	var (
		store *repository.Store
		err   error
	)
	if concurrent {
		store, err = repository.OpenConcurrent(repository.FileDSN(t.TempDir() + "/wallet.db"))
	} else {
		store, err = repository.Open(repository.MemoryDSN(t.Name()))
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Migrate(); err != nil {
		t.Fatal(err)
	}
	return service.New(store, nil), store
}

func seedWallets(t *testing.T, svc *service.Service, balances map[string]int64) {
	t.Helper()
	ctx := context.Background()
	for id, bal := range balances {
		if _, err := svc.CreateWallet(ctx, id, bal); err != nil {
			t.Fatalf("create wallet %s: %v", id, err)
		}
	}
}

func TestCreateTransferMovesMoneyAndWritesBalancedLedger(t *testing.T) {
	svc, store := newService(t, false)
	ctx := context.Background()
	seedWallets(t, svc, map[string]int64{"wallet_1": 500, "wallet_2": 50})

	out, err := svc.CreateTransfer(ctx, service.CreateTransferRequest{
		IdempotencyKey: "abc123",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         100,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Replay || out.Transfer.Status != domain.StatusProcessed {
		t.Fatalf("unexpected result: %+v", out)
	}

	from, err := svc.GetWallet(ctx, "wallet_1")
	if err != nil {
		t.Fatal(err)
	}
	to, err := svc.GetWallet(ctx, "wallet_2")
	if err != nil {
		t.Fatal(err)
	}
	if from.Balance != 400 || to.Balance != 150 {
		t.Fatalf("balances from=%d to=%d", from.Balance, to.Balance)
	}

	entries, err := store.ListLedgerByTransfer(ctx, out.Transfer.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("ledger entries = %d", len(entries))
	}
	var debit, credit domain.LedgerEntry
	for _, e := range entries {
		switch e.Type {
		case domain.EntryDebit:
			debit = e
		case domain.EntryCredit:
			credit = e
		}
	}
	if !debit.Balances(credit) {
		t.Fatalf("ledger does not balance: %+v %+v", debit, credit)
	}
	if debit.WalletID != "wallet_1" || credit.WalletID != "wallet_2" || debit.Amount != 100 {
		t.Fatalf("unexpected posting debit=%+v credit=%+v", debit, credit)
	}
}

func TestCreateTransferIsIdempotent(t *testing.T) {
	svc, store := newService(t, false)
	ctx := context.Background()
	seedWallets(t, svc, map[string]int64{"wallet_1": 500, "wallet_2": 0})

	req := service.CreateTransferRequest{
		IdempotencyKey: "same-key",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         40,
	}
	first, err := svc.CreateTransfer(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.CreateTransfer(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if !second.Replay {
		t.Fatal("second call should be a replay")
	}
	if first.Transfer.ID != second.Transfer.ID {
		t.Fatalf("replay returned different transfer %s vs %s", first.Transfer.ID, second.Transfer.ID)
	}

	from, err := svc.GetWallet(ctx, "wallet_1")
	if err != nil {
		t.Fatal(err)
	}
	if from.Balance != 460 {
		t.Fatalf("balance = %d, duplicate debit?", from.Balance)
	}
	n, err := store.CountTransfers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("transfers = %d", n)
	}
	entries, err := store.CountLedgerEntries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if entries != 2 {
		t.Fatalf("ledger entries = %d", entries)
	}
}

func TestCreateTransferRejectsReusedKeyWithDifferentPayload(t *testing.T) {
	svc, _ := newService(t, false)
	ctx := context.Background()
	seedWallets(t, svc, map[string]int64{"wallet_1": 500, "wallet_2": 0, "wallet_3": 0})

	_, err := svc.CreateTransfer(ctx, service.CreateTransferRequest{
		IdempotencyKey: "shared",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         10,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.CreateTransfer(ctx, service.CreateTransferRequest{
		IdempotencyKey: "shared",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_3",
		Amount:         10,
	})
	if !errors.Is(err, domain.ErrIdempotencyConflict) {
		t.Fatalf("got %v want conflict", err)
	}
}

func TestCreateTransferFailsWithoutSpendingWhenFundsAreShort(t *testing.T) {
	svc, store := newService(t, false)
	ctx := context.Background()
	seedWallets(t, svc, map[string]int64{"wallet_1": 30, "wallet_2": 0})

	out, err := svc.CreateTransfer(ctx, service.CreateTransferRequest{
		IdempotencyKey: "too-big",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         31,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Transfer.Status != domain.StatusFailed || out.Transfer.FailureReason != "insufficient_funds" {
		t.Fatalf("unexpected failure record: %+v", out.Transfer)
	}

	from, err := svc.GetWallet(ctx, "wallet_1")
	if err != nil {
		t.Fatal(err)
	}
	to, err := svc.GetWallet(ctx, "wallet_2")
	if err != nil {
		t.Fatal(err)
	}
	if from.Balance != 30 || to.Balance != 0 {
		t.Fatalf("balances changed on failure: from=%d to=%d", from.Balance, to.Balance)
	}
	entries, err := store.CountLedgerEntries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if entries != 0 {
		t.Fatalf("failed transfer wrote ledger rows: %d", entries)
	}

	replay, err := svc.CreateTransfer(ctx, service.CreateTransferRequest{
		IdempotencyKey: "too-big",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         31,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !replay.Replay || replay.Transfer.ID != out.Transfer.ID {
		t.Fatalf("failed transfer was not replayed: %+v", replay)
	}
}

func TestCreateTransferUnknownWalletDoesNotConsumeKey(t *testing.T) {
	svc, _ := newService(t, false)
	ctx := context.Background()
	seedWallets(t, svc, map[string]int64{"wallet_1": 100})

	_, err := svc.CreateTransfer(ctx, service.CreateTransferRequest{
		IdempotencyKey: "later",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_missing",
		Amount:         10,
	})
	if !errors.Is(err, domain.ErrWalletNotFound) {
		t.Fatalf("got %v", err)
	}

	if _, err := svc.CreateWallet(ctx, "wallet_missing", 0); err != nil {
		t.Fatal(err)
	}
	out, err := svc.CreateTransfer(ctx, service.CreateTransferRequest{
		IdempotencyKey: "later",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_missing",
		Amount:         10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Transfer.Status != domain.StatusProcessed {
		t.Fatalf("key should still be usable: %+v", out.Transfer)
	}
}

func TestConcurrentDebitsDoNotOverdraw(t *testing.T) {
	svc, store := newService(t, true)
	ctx := context.Background()
	seedWallets(t, svc, map[string]int64{"wallet_src": 100, "wallet_dst": 0})

	const workers = 20
	const amount int64 = 10
	var okCount atomic.Int64
	var failCount atomic.Int64
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		i := i
		go func() {
			defer wg.Done()
			out, err := svc.CreateTransfer(ctx, service.CreateTransferRequest{
				IdempotencyKey: fmt.Sprintf("conc-%d", i),
				FromWalletID:   "wallet_src",
				ToWalletID:     "wallet_dst",
				Amount:         amount,
			})
			if err != nil {
				t.Errorf("worker %d: %v", i, err)
				return
			}
			switch out.Transfer.Status {
			case domain.StatusProcessed:
				okCount.Add(1)
			case domain.StatusFailed:
				failCount.Add(1)
			default:
				t.Errorf("worker %d unexpected status %s", i, out.Transfer.Status)
			}
		}()
	}
	wg.Wait()

	if okCount.Load() != 10 || failCount.Load() != 10 {
		t.Fatalf("processed=%d failed=%d", okCount.Load(), failCount.Load())
	}

	src, err := svc.GetWallet(ctx, "wallet_src")
	if err != nil {
		t.Fatal(err)
	}
	dst, err := svc.GetWallet(ctx, "wallet_dst")
	if err != nil {
		t.Fatal(err)
	}
	if src.Balance != 0 || dst.Balance != 100 {
		t.Fatalf("final balances src=%d dst=%d", src.Balance, dst.Balance)
	}
	entries, err := store.CountLedgerEntries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if entries != 20 {
		t.Fatalf("ledger entries = %d", entries)
	}
}

func TestConcurrentReplayOfSameKeyCreatesOneTransfer(t *testing.T) {
	svc, store := newService(t, true)
	ctx := context.Background()
	seedWallets(t, svc, map[string]int64{"wallet_src": 80, "wallet_dst": 0})

	const workers = 12
	ids := make([]string, workers)
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		i := i
		go func() {
			defer wg.Done()
			out, err := svc.CreateTransfer(ctx, service.CreateTransferRequest{
				IdempotencyKey: "one-key",
				FromWalletID:   "wallet_src",
				ToWalletID:     "wallet_dst",
				Amount:         25,
			})
			if err != nil {
				t.Errorf("worker %d: %v", i, err)
				return
			}
			ids[i] = out.Transfer.ID
		}()
	}
	wg.Wait()

	first := ""
	for _, id := range ids {
		if id == "" {
			t.Fatal("missing transfer id")
		}
		if first == "" {
			first = id
		}
		if id != first {
			t.Fatalf("duplicate side effect: %s vs %s", first, id)
		}
	}
	n, err := store.CountTransfers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("transfers = %d", n)
	}
	src, err := svc.GetWallet(ctx, "wallet_src")
	if err != nil {
		t.Fatal(err)
	}
	if src.Balance != 55 {
		t.Fatalf("balance = %d", src.Balance)
	}
}

func TestCreateWalletRejectsDuplicate(t *testing.T) {
	svc, _ := newService(t, false)
	ctx := context.Background()
	if _, err := svc.CreateWallet(ctx, "w", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateWallet(ctx, "w", 1); !errors.Is(err, domain.ErrWalletExists) {
		t.Fatalf("got %v", err)
	}
}
