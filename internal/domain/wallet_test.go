package domain_test

import (
	"errors"
	"testing"

	"github.com/gaurav2204bits-collab/wallet-transfer-assignment/internal/domain"
)

func TestWalletDebit(t *testing.T) {
	t.Parallel()

	t.Run("debit within balance", func(t *testing.T) {
		t.Parallel()
		w := &domain.Wallet{ID: "w1", Balance: usd(100)}
		if err := w.Debit(usd(40)); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if w.Balance.Amount != 60 {
			t.Fatalf("balance = %d, want 60", w.Balance.Amount)
		}
	})

	t.Run("debit beyond balance is rejected and leaves balance untouched", func(t *testing.T) {
		t.Parallel()
		w := &domain.Wallet{ID: "w1", Balance: usd(100)}
		err := w.Debit(usd(150))
		if !errors.Is(err, domain.ErrInsufficientFunds) {
			t.Fatalf("got %v, want ErrInsufficientFunds", err)
		}
		if w.Balance.Amount != 100 {
			t.Fatalf("balance mutated to %d", w.Balance.Amount)
		}
	})

	t.Run("non-positive debit is a validation error", func(t *testing.T) {
		t.Parallel()
		w := &domain.Wallet{ID: "w1", Balance: usd(100)}
		if err := w.Debit(usd(0)); !errors.Is(err, domain.ErrValidation) {
			t.Fatalf("got %v, want ErrValidation", err)
		}
	})
}

func TestWalletCredit(t *testing.T) {
	t.Parallel()
	w := &domain.Wallet{ID: "w1", Balance: usd(100)}
	if err := w.Credit(usd(25)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if w.Balance.Amount != 125 {
		t.Fatalf("balance = %d, want 125", w.Balance.Amount)
	}
}
