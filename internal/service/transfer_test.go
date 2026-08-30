package service_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/codejk-crypto/wallet-transfer-assignment/internal/domain"
	"github.com/codejk-crypto/wallet-transfer-assignment/internal/repository"
	"github.com/codejk-crypto/wallet-transfer-assignment/internal/service"
	"github.com/codejk-crypto/wallet-transfer-assignment/internal/testutil"
)

func TestTransferHappyPath(t *testing.T) {
	ctx := context.Background()
	svc := service.New(testutil.Pool(t), nil)

	mustWallet(t, svc, "wallet_1", 100)
	mustWallet(t, svc, "wallet_2", 0)

	tr, status, err := svc.CreateTransfer(ctx, domain.CreateTransferInput{
		IdempotencyKey: "abc123",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         100,
	})
	if err != nil {
		t.Fatal(err)
	}
	if status != 201 || tr.Status != domain.StatusProcessed {
		t.Fatalf("status=%d transfer=%+v", status, tr)
	}

	from, _ := svc.GetWallet(ctx, "wallet_1")
	to, _ := svc.GetWallet(ctx, "wallet_2")
	if from.Balance != 0 || to.Balance != 100 {
		t.Fatalf("balances from=%d to=%d", from.Balance, to.Balance)
	}

	entries, err := repository.ListLedgerByTransfer(ctx, svc.Pool(), tr.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 ledger entries, got %d", len(entries))
	}
	var debit, credit int64
	for _, e := range entries {
		if e.Amount != 100 {
			t.Fatalf("entry amount %d", e.Amount)
		}
		switch e.Type {
		case domain.EntryDebit:
			if e.WalletID != "wallet_1" {
				t.Fatalf("debit wallet %s", e.WalletID)
			}
			debit += e.Amount
		case domain.EntryCredit:
			if e.WalletID != "wallet_2" {
				t.Fatalf("credit wallet %s", e.WalletID)
			}
			credit += e.Amount
		}
	}
	if debit != credit {
		t.Fatalf("ledger imbalance debit=%d credit=%d", debit, credit)
	}
}

func TestIdempotentReplay(t *testing.T) {
	ctx := context.Background()
	svc := service.New(testutil.Pool(t), nil)
	mustWallet(t, svc, "a", 50)
	mustWallet(t, svc, "b", 0)

	in := domain.CreateTransferInput{IdempotencyKey: "same", FromWalletID: "a", ToWalletID: "b", Amount: 10}
	first, _, err := svc.CreateTransfer(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := svc.CreateTransfer(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID {
		t.Fatalf("expected same transfer id, got %s and %s", first.ID, second.ID)
	}
	n, err := repository.CountTransfers(ctx, svc.Pool())
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("expected 1 transfer, got %d", n)
	}
	from, _ := svc.GetWallet(ctx, "a")
	if from.Balance != 40 {
		t.Fatalf("balance deducted twice: %d", from.Balance)
	}
}

func TestIdempotencyKeyConflict(t *testing.T) {
	ctx := context.Background()
	svc := service.New(testutil.Pool(t), nil)
	mustWallet(t, svc, "a", 50)
	mustWallet(t, svc, "b", 0)
	mustWallet(t, svc, "c", 0)

	_, _, err := svc.CreateTransfer(ctx, domain.CreateTransferInput{
		IdempotencyKey: "k1", FromWalletID: "a", ToWalletID: "b", Amount: 5,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = svc.CreateTransfer(ctx, domain.CreateTransferInput{
		IdempotencyKey: "k1", FromWalletID: "a", ToWalletID: "c", Amount: 5,
	})
	if !errors.Is(err, domain.ErrIdempotencyConflict) {
		t.Fatalf("expected conflict, got %v", err)
	}
}

func TestInsufficientFunds(t *testing.T) {
	ctx := context.Background()
	svc := service.New(testutil.Pool(t), nil)
	mustWallet(t, svc, "a", 5)
	mustWallet(t, svc, "b", 0)

	tr, status, err := svc.CreateTransfer(ctx, domain.CreateTransferInput{
		IdempotencyKey: "fail-1", FromWalletID: "a", ToWalletID: "b", Amount: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if status != 422 || tr.Status != domain.StatusFailed {
		t.Fatalf("status=%d transfer=%+v", status, tr)
	}
	entries, err := repository.ListLedgerByTransfer(ctx, svc.Pool(), tr.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("failed transfer must not write ledger, got %d", len(entries))
	}
	from, _ := svc.GetWallet(ctx, "a")
	to, _ := svc.GetWallet(ctx, "b")
	if from.Balance != 5 || to.Balance != 0 {
		t.Fatalf("balances changed on failure from=%d to=%d", from.Balance, to.Balance)
	}

	again, status2, err := svc.CreateTransfer(ctx, domain.CreateTransferInput{
		IdempotencyKey: "fail-1", FromWalletID: "a", ToWalletID: "b", Amount: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if again.ID != tr.ID || status2 != 422 {
		t.Fatalf("replay should return original failed transfer, got %+v status %d", again, status2)
	}
}

func TestConcurrentSameIdempotencyKey(t *testing.T) {
	ctx := context.Background()
	svc := service.New(testutil.Pool(t), nil)
	mustWallet(t, svc, "a", 100)
	mustWallet(t, svc, "b", 0)

	const n = 8
	var wg sync.WaitGroup
	ids := make([]string, n)
	errs := make([]error, n)
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			tr, _, err := svc.CreateTransfer(ctx, domain.CreateTransferInput{
				IdempotencyKey: "concurrent-key",
				FromWalletID:   "a",
				ToWalletID:     "b",
				Amount:         25,
			})
			errs[i] = err
			if err == nil {
				ids[i] = tr.ID
			}
		}(i)
	}
	wg.Wait()
	var got string
	for i, err := range errs {
		if err != nil {
			t.Fatalf("goroutine %d: %v", i, err)
		}
		if got == "" {
			got = ids[i]
		} else if ids[i] != got {
			t.Fatalf("multiple transfer ids: %s and %s", got, ids[i])
		}
	}
	count, _ := repository.CountTransfers(ctx, svc.Pool())
	if count != 1 {
		t.Fatalf("expected 1 transfer, got %d", count)
	}
	from, _ := svc.GetWallet(ctx, "a")
	if from.Balance != 75 {
		t.Fatalf("expected balance 75, got %d", from.Balance)
	}
}

func TestConcurrentDebitsNoOverdraft(t *testing.T) {
	ctx := context.Background()
	svc := service.New(testutil.Pool(t), nil)
	mustWallet(t, svc, "source", 100)
	mustWallet(t, svc, "d1", 0)
	mustWallet(t, svc, "d2", 0)

	var wg sync.WaitGroup
	results := make([]domain.Transfer, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		tr, _, err := svc.CreateTransfer(ctx, domain.CreateTransferInput{
			IdempotencyKey: "c1", FromWalletID: "source", ToWalletID: "d1", Amount: 80,
		})
		if err != nil {
			t.Error(err)
			return
		}
		results[0] = tr
	}()
	go func() {
		defer wg.Done()
		tr, _, err := svc.CreateTransfer(ctx, domain.CreateTransferInput{
			IdempotencyKey: "c2", FromWalletID: "source", ToWalletID: "d2", Amount: 80,
		})
		if err != nil {
			t.Error(err)
			return
		}
		results[1] = tr
	}()
	wg.Wait()

	processed := 0
	failed := 0
	for _, tr := range results {
		switch tr.Status {
		case domain.StatusProcessed:
			processed++
		case domain.StatusFailed:
			failed++
		}
	}
	if processed != 1 || failed != 1 {
		t.Fatalf("expected 1 processed and 1 failed, got processed=%d failed=%d %+v", processed, failed, results)
	}
	source, _ := svc.GetWallet(ctx, "source")
	if source.Balance != 20 {
		t.Fatalf("source balance=%d", source.Balance)
	}
}

func TestUnknownWallet(t *testing.T) {
	ctx := context.Background()
	svc := service.New(testutil.Pool(t), nil)
	mustWallet(t, svc, "a", 10)
	_, _, err := svc.CreateTransfer(ctx, domain.CreateTransferInput{
		IdempotencyKey: "missing", FromWalletID: "a", ToWalletID: "nope", Amount: 1,
	})
	if !errors.Is(err, domain.ErrWalletNotFound) {
		t.Fatalf("got %v", err)
	}
}

func mustWallet(t *testing.T, svc *service.Service, id string, balance int64) {
	t.Helper()
	if _, err := svc.CreateWallet(context.Background(), domain.CreateWalletInput{ID: id, Balance: balance}); err != nil {
		t.Fatal(err)
	}
}
