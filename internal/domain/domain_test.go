package domain_test

import (
	"math"
	"strings"
	"testing"

	"wallet-transfer-assignment/internal/domain"
)

func TestWallet_DebitAndCredit(t *testing.T) {
	w := &domain.Wallet{
		ID:      "w1",
		Balance: 100,
	}

	// Test successful debit
	if err := w.Debit(40); err != nil {
		t.Fatalf("unexpected debit error: %v", err)
	}
	if w.Balance != 60 {
		t.Fatalf("expected balance 60, got %d", w.Balance)
	}

	// Test debiting more than balance
	if err := w.Debit(70); err != domain.ErrInsufficientFunds {
		t.Fatalf("expected ErrInsufficientFunds, got %v", err)
	}

	// Test negative debit amount
	if err := w.Debit(-10); err != domain.ErrInvalidAmount {
		t.Fatalf("expected ErrInvalidAmount, got %v", err)
	}

	// Test credit
	if err := w.Credit(50); err != nil {
		t.Fatalf("unexpected credit error: %v", err)
	}
	if w.Balance != 110 {
		t.Fatalf("expected balance 110, got %d", w.Balance)
	}

	// Test negative credit amount
	if err := w.Credit(-10); err != domain.ErrInvalidAmount {
		t.Fatalf("expected ErrInvalidAmount, got %v", err)
	}

	// Test CanDebit predicate
	wallet := &domain.Wallet{ID: "w2", Balance: 100}
	if !wallet.CanDebit(50) {
		t.Fatalf("expected CanDebit(50) to be true for balance 100")
	}
	if !wallet.CanDebit(100) {
		t.Fatalf("expected CanDebit(100) to be true for balance 100")
	}
	if wallet.CanDebit(150) {
		t.Fatalf("expected CanDebit(150) to be false for balance 100")
	}
	if wallet.CanDebit(0) {
		t.Fatalf("expected CanDebit(0) to be false (amount must be strictly positive)")
	}
	if wallet.CanDebit(-10) {
		t.Fatalf("expected CanDebit(-10) to be false for negative amount")
	}

	// Test CanCredit predicate
	if !wallet.CanCredit(50) {
		t.Fatalf("expected CanCredit(50) to be true")
	}
	if wallet.CanCredit(0) {
		t.Fatalf("expected CanCredit(0) to be false (amount must be strictly positive)")
	}
	if wallet.CanCredit(-10) {
		t.Fatalf("expected CanCredit(-10) to be false")
	}
	if err := w.Credit(0); err != domain.ErrInvalidAmount {
		t.Fatalf("expected ErrInvalidAmount for 0, got %v", err)
	}

	// Test CanCredit and Credit overflow
	w.Balance = math.MaxInt64 - 50
	if !w.CanCredit(50) {
		t.Fatalf("expected CanCredit(50) to be true")
	}
	if w.CanCredit(51) {
		t.Fatalf("expected CanCredit(51) to be false due to overflow")
	}
	if w.CanCredit(0) {
		t.Fatalf("expected CanCredit(0) to be false")
	}
	if w.CanCredit(-10) {
		t.Fatalf("expected CanCredit(-10) to be false")
	}

	if err := w.Credit(50); err != nil {
		t.Fatalf("unexpected error crediting up to MaxInt64: %v", err)
	}
	if w.Balance != math.MaxInt64 {
		t.Fatalf("expected balance to be math.MaxInt64, got %d", w.Balance)
	}

	if err := w.Credit(1); err != domain.ErrBalanceOverflow {
		t.Fatalf("expected ErrBalanceOverflow, got %v", err)
	}
}

func TestTransfer_Transitions(t *testing.T) {
	tr := &domain.Transfer{
		ID:             "t1",
		IdempotencyKey: "k1",
		FromWalletID:   "w1",
		ToWalletID:     "w2",
		Amount:         100,
		Status:         domain.TransferStatusPending,
	}

	if err := tr.Validate(); err != nil {
		t.Fatalf("validation failed: %v", err)
	}

	// Transition to processed
	if err := tr.MarkProcessed(); err != nil {
		t.Fatalf("failed to mark processed: %v", err)
	}
	if tr.Status != domain.TransferStatusProcessed {
		t.Fatalf("expected status PROCESSED, got %s", tr.Status)
	}

	// Re-transitioning should fail
	if err := tr.MarkProcessed(); err != domain.ErrInvalidStateTransition {
		t.Fatalf("expected ErrInvalidStateTransition, got %v", err)
	}
}

func TestTransfer_ValidationErrors(t *testing.T) {
	// Same wallet
	tr := &domain.Transfer{
		ID:             "t1",
		IdempotencyKey: "k1",
		FromWalletID:   "w1",
		ToWalletID:     "w1",
		Amount:         100,
	}
	if err := tr.Validate(); err != domain.ErrSameWalletTransfer {
		t.Fatalf("expected ErrSameWalletTransfer, got %v", err)
	}

	// Zero amount
	tr.ToWalletID = "w2"
	tr.Amount = 0
	if err := tr.Validate(); err != domain.ErrInvalidAmount {
		t.Fatalf("expected ErrInvalidAmount, got %v", err)
	}

	// Missing idempotency key
	tr.Amount = 100
	tr.IdempotencyKey = ""
	if err := tr.Validate(); err != domain.ErrMissingIdempotencyKey {
		t.Fatalf("expected ErrMissingIdempotencyKey, got %v", err)
	}

	// Overlong idempotency key (> 128 chars)
	tr.IdempotencyKey = strings.Repeat("a", 129)
	if err := tr.Validate(); err != domain.ErrInvalidIdempotencyKey {
		t.Fatalf("expected ErrInvalidIdempotencyKey, got %v", err)
	}

	// Missing from_wallet_id
	tr.IdempotencyKey = "k1"
	tr.FromWalletID = ""
	if err := tr.Validate(); err != domain.ErrMissingWalletID {
		t.Fatalf("expected ErrMissingWalletID, got %v", err)
	}

	// Missing to_wallet_id
	tr.FromWalletID = "w1"
	tr.ToWalletID = ""
	if err := tr.Validate(); err != domain.ErrMissingWalletID {
		t.Fatalf("expected ErrMissingWalletID, got %v", err)
	}
}

func TestLedger_NewDoubleEntryPair(t *testing.T) {
	debit, credit, err := domain.NewDoubleEntryPair("t1", "w1", "w2", 500)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if debit.Type != domain.LedgerEntryTypeDebit || debit.WalletID != "w1" || debit.Amount != 500 {
		t.Fatalf("invalid debit entry: %+v", debit)
	}
	if credit.Type != domain.LedgerEntryTypeCredit || credit.WalletID != "w2" || credit.Amount != 500 {
		t.Fatalf("invalid credit entry: %+v", credit)
	}
}

func TestIdempotency_ComputeRequestHash(t *testing.T) {
	h1 := domain.ComputeRequestHash("w1", "w2", 100)
	h2 := domain.ComputeRequestHash("w1", "w2", 100)
	h3 := domain.ComputeRequestHash("w1", "w2", 200)

	if h1 != h2 {
		t.Fatalf("expected hashes to match for identical inputs")
	}
	if h1 == h3 {
		t.Fatalf("expected different hashes for different amounts")
	}

	// Verify length-delimited encoding prevents delimiter-injection collisions:
	// (from="a", to="b:c") vs (from="a:b", to="c")
	hCol1 := domain.ComputeRequestHash("a", "b:c", 100)
	hCol2 := domain.ComputeRequestHash("a:b", "c", 100)
	if hCol1 == hCol2 {
		t.Fatalf("expected different hashes for delimiter-containing inputs")
	}
}
