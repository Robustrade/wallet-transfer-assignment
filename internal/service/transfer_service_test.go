package service_test

import (
	"context"
	"testing"

	"github.com/example/wallet-transfer/internal/domain"
	"github.com/example/wallet-transfer/internal/repository/memory"
	"github.com/example/wallet-transfer/internal/service"
)

// newFixture spins up a fresh in-memory store with two seeded wallets,
// shared by every test below.
func newFixture() (*service.TransferService, *memory.Store) {
	store := memory.NewStore()
	store.SeedWallet(domain.Wallet{ID: "wallet_1", Balance: 500})
	store.SeedWallet(domain.Wallet{ID: "wallet_2", Balance: 100})
	return service.NewTransferService(store), store
}

func TestCreateTransfer_Success(t *testing.T) {
	svc, store := newFixture()

	result, err := svc.CreateTransfer(context.Background(), service.CreateTransferRequest{
		IdempotencyKey: "key-1",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         100,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.State != domain.TransferProcessed {
		t.Fatalf("expected PROCESSED, got %s", result.State)
	}

	from, _ := store.GetWallet("wallet_1")
	to, _ := store.GetWallet("wallet_2")
	if from.Balance != 400 {
		t.Errorf("expected wallet_1 balance 400, got %d", from.Balance)
	}
	if to.Balance != 200 {
		t.Errorf("expected wallet_2 balance 200, got %d", to.Balance)
	}
}

func TestCreateTransfer_InsufficientFunds(t *testing.T) {
	svc, store := newFixture()

	result, err := svc.CreateTransfer(context.Background(), service.CreateTransferRequest{
		IdempotencyKey: "key-2",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         999,
	})
	// Insufficient funds is a business outcome, not a transport error.
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.State != domain.TransferFailed {
		t.Fatalf("expected FAILED, got %s", result.State)
	}

	// Balances must be completely untouched by a failed transfer.
	from, _ := store.GetWallet("wallet_1")
	to, _ := store.GetWallet("wallet_2")
	if from.Balance != 500 || to.Balance != 100 {
		t.Errorf("balances changed on a failed transfer: from=%d to=%d", from.Balance, to.Balance)
	}
}

func TestCreateTransfer_IdempotentReplay(t *testing.T) {
	svc, store := newFixture()
	req := service.CreateTransferRequest{
		IdempotencyKey: "key-3",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         50,
	}

	first, err := svc.CreateTransfer(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Simulate a client retrying after a timeout/dropped response several
	// times over — every retry must return the exact same transfer.
	for i := 0; i < 3; i++ {
		second, err := svc.CreateTransfer(context.Background(), req)
		if err != nil {
			t.Fatalf("unexpected error on retry %d: %v", i, err)
		}
		if second.TransferID != first.TransferID {
			t.Fatalf("retry %d produced a new transfer: %s vs %s", i, second.TransferID, first.TransferID)
		}
	}

	// Exactly one debit/credit pair should ever have been applied.
	from, _ := store.GetWallet("wallet_1")
	if from.Balance != 450 {
		t.Errorf("expected wallet_1 balance 450 after retries, got %d", from.Balance)
	}
}

func TestCreateTransfer_FailedTransferIsAlsoIdempotent(t *testing.T) {
	svc, _ := newFixture()
	req := service.CreateTransferRequest{
		IdempotencyKey: "key-fail-retry",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         999, // more than wallet_1 has
	}

	first, err := svc.CreateTransfer(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	second, err := svc.CreateTransfer(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error on retry: %v", err)
	}
	if second.TransferID != first.TransferID || second.State != domain.TransferFailed {
		t.Fatalf("expected the same cached FAILED result, got %+v vs %+v", first, second)
	}
}

func TestCreateTransfer_IdempotencyKeyReusedWithDifferentPayload(t *testing.T) {
	svc, _ := newFixture()
	key := "key-4"

	if _, err := svc.CreateTransfer(context.Background(), service.CreateTransferRequest{
		IdempotencyKey: key, FromWalletID: "wallet_1", ToWalletID: "wallet_2", Amount: 10,
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Reusing the same key with a different amount is a client bug we want
	// to surface loudly, not silently reinterpret as a retry.
	_, err := svc.CreateTransfer(context.Background(), service.CreateTransferRequest{
		IdempotencyKey: key, FromWalletID: "wallet_1", ToWalletID: "wallet_2", Amount: 999,
	})
	if err != domain.ErrIdempotencyKeyConflict {
		t.Fatalf("expected ErrIdempotencyKeyConflict, got %v", err)
	}
}

func TestCreateTransfer_ValidationErrors(t *testing.T) {
	svc, _ := newFixture()
	cases := []struct {
		name string
		req  service.CreateTransferRequest
		want error
	}{
		{"missing key", service.CreateTransferRequest{FromWalletID: "wallet_1", ToWalletID: "wallet_2", Amount: 10}, domain.ErrIdempotencyKeyRequired},
		{"zero amount", service.CreateTransferRequest{IdempotencyKey: "k1", FromWalletID: "wallet_1", ToWalletID: "wallet_2", Amount: 0}, domain.ErrInvalidAmount},
		{"negative amount", service.CreateTransferRequest{IdempotencyKey: "k2", FromWalletID: "wallet_1", ToWalletID: "wallet_2", Amount: -5}, domain.ErrInvalidAmount},
		{"same wallet", service.CreateTransferRequest{IdempotencyKey: "k3", FromWalletID: "wallet_1", ToWalletID: "wallet_1", Amount: 10}, domain.ErrSameWallet},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.CreateTransfer(context.Background(), tc.req)
			if err != tc.want {
				t.Fatalf("expected %v, got %v", tc.want, err)
			}
		})
	}
}

func TestCreateTransfer_UnknownWallet(t *testing.T) {
	svc, _ := newFixture()
	_, err := svc.CreateTransfer(context.Background(), service.CreateTransferRequest{
		IdempotencyKey: "key-unknown",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_does_not_exist",
		Amount:         10,
	})
	if err != domain.ErrWalletNotFound {
		t.Fatalf("expected ErrWalletNotFound, got %v", err)
	}
}

func TestCreateTransfer_LedgerBalances(t *testing.T) {
	svc, store := newFixture()
	_, err := svc.CreateTransfer(context.Background(), service.CreateTransferRequest{
		IdempotencyKey: "key-5", FromWalletID: "wallet_1", ToWalletID: "wallet_2", Amount: 75,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	entries := store.Ledger()
	if len(entries) != 2 {
		t.Fatalf("expected exactly 2 ledger entries, got %d", len(entries))
	}

	var debit, credit domain.LedgerEntry
	for _, e := range entries {
		if e.Type == domain.LedgerDebit {
			debit = e
		} else if e.Type == domain.LedgerCredit {
			credit = e
		}
	}
	if debit.Amount != 75 || credit.Amount != 75 {
		t.Fatalf("ledger does not balance: debit=%d credit=%d", debit.Amount, credit.Amount)
	}
	if debit.WalletID != "wallet_1" || credit.WalletID != "wallet_2" {
		t.Fatalf("ledger entries point at the wrong wallets: debit=%s credit=%s", debit.WalletID, credit.WalletID)
	}
	if debit.TransferID != credit.TransferID {
		t.Fatalf("debit and credit entries reference different transfers")
	}
}
