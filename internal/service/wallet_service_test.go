package service_test

import (
	"errors"
	"testing"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
)

func TestCreateWallet(t *testing.T) {
	_, walletSvc, _ := newTestEnv(t)

	w, err := walletSvc.CreateWallet(ctx(), 500, "USD")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if w.Balance != 500 || w.Currency != "USD" {
		t.Fatalf("unexpected wallet: %+v", w)
	}
	if w.ID == "" {
		t.Fatalf("expected a server-generated wallet id, got empty")
	}
}

func TestCreateWallet_GeneratesUniqueIDs(t *testing.T) {
	_, walletSvc, _ := newTestEnv(t)

	first, err := walletSvc.CreateWallet(ctx(), 100, "USD")
	if err != nil {
		t.Fatalf("first create: %v", err)
	}
	second, err := walletSvc.CreateWallet(ctx(), 100, "USD")
	if err != nil {
		t.Fatalf("second create: %v", err)
	}
	if first.ID == second.ID {
		t.Fatalf("each wallet must get a distinct id, got %s twice", first.ID)
	}
}

func TestCreateWallet_NegativeOpeningBalanceRejected(t *testing.T) {
	_, walletSvc, _ := newTestEnv(t)

	_, err := walletSvc.CreateWallet(ctx(), -100, "USD")
	if !domain.IsValidation(err) {
		t.Fatalf("expected validation error, got %v", err)
	}
}

func TestGetWallet_NotFound(t *testing.T) {
	_, walletSvc, _ := newTestEnv(t)

	_, err := walletSvc.GetWallet(ctx(), "missing")
	if !errors.Is(err, domain.ErrWalletNotFound) {
		t.Fatalf("expected ErrWalletNotFound, got %v", err)
	}
}

func TestListLedger_ReturnsHistoryNewestFirst(t *testing.T) {
	transferSvc, walletSvc, _ := newTestEnv(t)
	mustCreateWallet(t, walletSvc, "w1", 1000)
	mustCreateWallet(t, walletSvc, "w2", 0)

	for i, amt := range []int64{100, 200} {
		_, err := transferSvc.CreateTransfer(ctx(), domain.TransferRequest{
			IdempotencyKey: keyFor(i), FromWalletID: "w1", ToWalletID: "w2", Amount: amt,
		})
		if err != nil {
			t.Fatalf("transfer %d: %v", i, err)
		}
	}

	entries, err := walletSvc.ListLedger(ctx(), "w1")
	if err != nil {
		t.Fatalf("list ledger: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 ledger entries, got %d", len(entries))
	}
	// Newest first: the 200 debit was recorded last, so it should appear first.
	if entries[0].Amount != 200 {
		t.Fatalf("expected newest entry (200) first, got %d", entries[0].Amount)
	}
}

func keyFor(i int) string {
	return "ledger-key-" + string(rune('a'+i))
}
