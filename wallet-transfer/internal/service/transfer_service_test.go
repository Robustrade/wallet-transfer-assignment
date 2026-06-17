package service_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/example/wallet-transfer/internal/domain"
	"github.com/example/wallet-transfer/internal/repository"
	"github.com/example/wallet-transfer/internal/service"
)

func newService() *service.TransferService {
	return service.NewTransferService(pool, repository.New())
}

func TestHappyPath(t *testing.T) {
	reset(t)
	seedWallet(t, "w1", 1000)
	seedWallet(t, "w2", 0)
	svc := newService()

	res, err := svc.CreateTransfer(context.Background(), service.Command{
		IdempotencyKey: "k1", FromWalletID: "w1", ToWalletID: "w2", Amount: 250,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.HTTPStatus != 201 {
		t.Fatalf("want 201, got %d", res.HTTPStatus)
	}
	if got := balance(t, "w1"); got != 750 {
		t.Errorf("w1 balance = %d, want 750", got)
	}
	if got := balance(t, "w2"); got != 250 {
		t.Errorf("w2 balance = %d, want 250", got)
	}
	if got := countLedgerEntries(t); got != 2 {
		t.Errorf("ledger entries = %d, want 2", got)
	}
	if got := countTransfersByStatus(t, string(domain.StatusProcessed)); got != 1 {
		t.Errorf("processed transfers = %d, want 1", got)
	}
}

func TestIdempotentReplay(t *testing.T) {
	reset(t)
	seedWallet(t, "w1", 1000)
	seedWallet(t, "w2", 0)
	svc := newService()
	cmd := service.Command{IdempotencyKey: "same", FromWalletID: "w1", ToWalletID: "w2", Amount: 300}

	first, err := svc.CreateTransfer(context.Background(), cmd)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	second, err := svc.CreateTransfer(context.Background(), cmd)
	if err != nil {
		t.Fatalf("second: %v", err)
	}

	if string(first.Body) != string(second.Body) {
		t.Errorf("replayed body differs:\n first=%s\nsecond=%s", first.Body, second.Body)
	}
	if !second.Replayed {
		t.Errorf("second call should be a replay")
	}
	// The transfer must have happened exactly once.
	if got := balance(t, "w1"); got != 700 {
		t.Errorf("w1 balance = %d, want 700 (no double apply)", got)
	}
	if got := countLedgerEntries(t); got != 2 {
		t.Errorf("ledger entries = %d, want 2 (no duplicate entries)", got)
	}
}

func TestIdempotencyConflict(t *testing.T) {
	reset(t)
	seedWallet(t, "w1", 1000)
	seedWallet(t, "w2", 0)
	svc := newService()

	_, err := svc.CreateTransfer(context.Background(), service.Command{
		IdempotencyKey: "reused", FromWalletID: "w1", ToWalletID: "w2", Amount: 100,
	})
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	// Same key, different amount -> conflict.
	_, err = svc.CreateTransfer(context.Background(), service.Command{
		IdempotencyKey: "reused", FromWalletID: "w1", ToWalletID: "w2", Amount: 999,
	})
	if !errors.Is(err, domain.ErrIdempotencyConflict) {
		t.Fatalf("want ErrIdempotencyConflict, got %v", err)
	}
	// The second request must not have moved any money.
	if got := balance(t, "w1"); got != 900 {
		t.Errorf("w1 balance = %d, want 900", got)
	}
}

func TestInsufficientFundsRecordsFailed(t *testing.T) {
	reset(t)
	seedWallet(t, "w1", 50)
	seedWallet(t, "w2", 0)
	svc := newService()

	res, err := svc.CreateTransfer(context.Background(), service.Command{
		IdempotencyKey: "k-fail", FromWalletID: "w1", ToWalletID: "w2", Amount: 100,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.HTTPStatus != 422 {
		t.Fatalf("want 422, got %d", res.HTTPStatus)
	}
	if got := balance(t, "w1"); got != 50 {
		t.Errorf("w1 balance changed: %d, want 50", got)
	}
	if got := countLedgerEntries(t); got != 0 {
		t.Errorf("ledger entries = %d, want 0 for a FAILED transfer", got)
	}
	if got := countTransfersByStatus(t, string(domain.StatusFailed)); got != 1 {
		t.Errorf("failed transfers = %d, want 1 (recorded FAILED)", got)
	}

	// A duplicate of a failed request replays the same 422.
	dup, err := svc.CreateTransfer(context.Background(), service.Command{
		IdempotencyKey: "k-fail", FromWalletID: "w1", ToWalletID: "w2", Amount: 100,
	})
	if err != nil {
		t.Fatalf("dup: %v", err)
	}
	if dup.HTTPStatus != 422 || !dup.Replayed {
		t.Errorf("dup = {status:%d replayed:%v}, want {422 true}", dup.HTTPStatus, dup.Replayed)
	}
}

func TestWalletNotFound(t *testing.T) {
	reset(t)
	seedWallet(t, "w1", 1000)
	svc := newService()

	_, err := svc.CreateTransfer(context.Background(), service.Command{
		IdempotencyKey: "k-missing", FromWalletID: "w1", ToWalletID: "ghost", Amount: 100,
	})
	if !errors.Is(err, domain.ErrWalletNotFound) {
		t.Fatalf("want ErrWalletNotFound, got %v", err)
	}
}

// TestConcurrencyNoDoubleSpend is the headline test: many concurrent transfers
// drain a single hot wallet. Exactly floor(balance/amount) must succeed, the
// balance must never go negative, and no double-spend may occur.
func TestConcurrencyNoDoubleSpend(t *testing.T) {
	reset(t)
	const (
		start    = 500
		amount   = 100
		attempts = 20
	)
	seedWallet(t, "hot", start)
	seedWallet(t, "dst", 0)
	svc := newService()

	var wg sync.WaitGroup
	var mu sync.Mutex
	succeeded := 0

	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			res, err := svc.CreateTransfer(context.Background(), service.Command{
				IdempotencyKey: key("c", i), FromWalletID: "hot", ToWalletID: "dst", Amount: amount,
			})
			if err != nil {
				t.Errorf("attempt %d: unexpected error: %v", i, err)
				return
			}
			if res.HTTPStatus == 201 {
				mu.Lock()
				succeeded++
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()

	wantSucceeded := start / amount // 5
	if succeeded != wantSucceeded {
		t.Errorf("succeeded = %d, want %d", succeeded, wantSucceeded)
	}
	if got := balance(t, "hot"); got != start-domain.Amount(wantSucceeded*amount) {
		t.Errorf("hot balance = %d, want %d", got, start-wantSucceeded*amount)
	}
	if got := balance(t, "hot"); got < 0 {
		t.Errorf("hot balance went negative: %d", got)
	}
	if got := balance(t, "dst"); got != domain.Amount(wantSucceeded*amount) {
		t.Errorf("dst balance = %d, want %d", got, wantSucceeded*amount)
	}
	if got := countLedgerEntries(t); got != wantSucceeded*2 {
		t.Errorf("ledger entries = %d, want %d", got, wantSucceeded*2)
	}
}

// TestLedgerReconciliation asserts the stored balance equals the balance derived
// from the ledger -- the two sources of truth must agree.
func TestLedgerReconciliation(t *testing.T) {
	reset(t)
	seedWallet(t, "a", 1000)
	seedWallet(t, "b", 1000)
	svc := newService()

	moves := []struct {
		key      string
		from, to string
		amount   domain.Amount
	}{
		{"r1", "a", "b", 100},
		{"r2", "b", "a", 50},
		{"r3", "a", "b", 200},
	}
	for _, m := range moves {
		if _, err := svc.CreateTransfer(context.Background(), service.Command{
			IdempotencyKey: m.key, FromWalletID: m.from, ToWalletID: m.to, Amount: m.amount,
		}); err != nil {
			t.Fatalf("move %s: %v", m.key, err)
		}
	}

	for _, id := range []string{"a", "b"} {
		// Derived = net of ledger entries; stored started at 1000, so
		// stored - 1000 must equal the derived net movement.
		stored := balance(t, id)
		derived := derivedBalance(t, id)
		if stored-1000 != derived {
			t.Errorf("wallet %s: stored-1000 (%d) != derived (%d)", id, stored-1000, derived)
		}
	}
}
