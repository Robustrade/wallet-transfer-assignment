package domain

import (
	"errors"
	"testing"
)

func TestTransferStateMachine(t *testing.T) {
	t.Run("pending transitions to processed", func(t *testing.T) {
		tr := NewTransfer("t1", "key1", "w1", "w2", 100)
		if err := tr.MarkProcessed(); err != nil {
			t.Fatalf("expected processed transition to succeed, got %v", err)
		}
		if tr.Status != StatusProcessed {
			t.Fatalf("expected status PROCESSED, got %s", tr.Status)
		}
	})

	t.Run("pending transitions to failed with reason", func(t *testing.T) {
		tr := NewTransfer("t1", "key1", "w1", "w2", 100)
		if err := tr.MarkFailed(ReasonInsufficientFunds); err != nil {
			t.Fatalf("expected failed transition to succeed, got %v", err)
		}
		if tr.Status != StatusFailed || tr.FailureReason != ReasonInsufficientFunds {
			t.Fatalf("unexpected state: %s/%s", tr.Status, tr.FailureReason)
		}
	})

	t.Run("processed cannot transition again", func(t *testing.T) {
		tr := NewTransfer("t1", "key1", "w1", "w2", 100)
		_ = tr.MarkProcessed()
		if err := tr.MarkProcessed(); !errors.Is(err, ErrInvalidStateTransition) {
			t.Fatalf("expected ErrInvalidStateTransition, got %v", err)
		}
		if err := tr.MarkFailed(ReasonInsufficientFunds); !errors.Is(err, ErrInvalidStateTransition) {
			t.Fatalf("expected ErrInvalidStateTransition, got %v", err)
		}
	})

	t.Run("failed cannot transition again", func(t *testing.T) {
		tr := NewTransfer("t1", "key1", "w1", "w2", 100)
		_ = tr.MarkFailed(ReasonInsufficientFunds)
		if err := tr.MarkProcessed(); !errors.Is(err, ErrInvalidStateTransition) {
			t.Fatalf("expected ErrInvalidStateTransition, got %v", err)
		}
	})
}

func TestTransferSameParams(t *testing.T) {
	base := NewTransfer("t1", "key1", "w1", "w2", 100)

	tests := []struct {
		name  string
		other *Transfer
		want  bool
	}{
		{"identical params", NewTransfer("t2", "key1", "w1", "w2", 100), true},
		{"different amount", NewTransfer("t2", "key1", "w1", "w2", 200), false},
		{"different source", NewTransfer("t2", "key1", "w3", "w2", 100), false},
		{"different destination", NewTransfer("t2", "key1", "w1", "w3", 100), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := base.SameParams(tc.other); got != tc.want {
				t.Fatalf("SameParams = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestWalletDebitCredit(t *testing.T) {
	t.Run("debit reduces balance", func(t *testing.T) {
		w, _ := NewWallet("w1", 100, "USD")
		if err := w.Debit(40); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if w.Balance != 60 {
			t.Fatalf("expected balance 60, got %d", w.Balance)
		}
	})

	t.Run("debit beyond balance is rejected", func(t *testing.T) {
		w, _ := NewWallet("w1", 100, "USD")
		if err := w.Debit(150); !errors.Is(err, ErrInsufficientFunds) {
			t.Fatalf("expected ErrInsufficientFunds, got %v", err)
		}
		if w.Balance != 100 {
			t.Fatalf("balance must be unchanged on failed debit, got %d", w.Balance)
		}
	})

	t.Run("debit of exact balance succeeds", func(t *testing.T) {
		w, _ := NewWallet("w1", 100, "USD")
		if err := w.Debit(100); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if w.Balance != 0 {
			t.Fatalf("expected balance 0, got %d", w.Balance)
		}
	})

	t.Run("credit increases balance", func(t *testing.T) {
		w, _ := NewWallet("w1", 100, "USD")
		if err := w.Credit(25); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if w.Balance != 125 {
			t.Fatalf("expected balance 125, got %d", w.Balance)
		}
	})

	t.Run("non-positive amounts are rejected", func(t *testing.T) {
		w, _ := NewWallet("w1", 100, "USD")
		if err := w.Debit(0); !IsValidation(err) {
			t.Fatalf("expected validation error for zero debit, got %v", err)
		}
		if err := w.Credit(-5); !IsValidation(err) {
			t.Fatalf("expected validation error for negative credit, got %v", err)
		}
	})
}

func TestTransferRequestValidate(t *testing.T) {
	valid := TransferRequest{IdempotencyKey: "k", FromWalletID: "w1", ToWalletID: "w2", Amount: 100}
	if err := valid.Validate(); err != nil {
		t.Fatalf("expected valid request, got %v", err)
	}

	tests := []struct {
		name string
		req  TransferRequest
	}{
		{"missing key", TransferRequest{FromWalletID: "w1", ToWalletID: "w2", Amount: 100}},
		{"missing source", TransferRequest{IdempotencyKey: "k", ToWalletID: "w2", Amount: 100}},
		{"missing destination", TransferRequest{IdempotencyKey: "k", FromWalletID: "w1", Amount: 100}},
		{"same wallet", TransferRequest{IdempotencyKey: "k", FromWalletID: "w1", ToWalletID: "w1", Amount: 100}},
		{"zero amount", TransferRequest{IdempotencyKey: "k", FromWalletID: "w1", ToWalletID: "w2", Amount: 0}},
		{"negative amount", TransferRequest{IdempotencyKey: "k", FromWalletID: "w1", ToWalletID: "w2", Amount: -1}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.req.Validate(); !IsValidation(err) {
				t.Fatalf("expected validation error, got %v", err)
			}
		})
	}
}

func TestNewLedgerPairBalances(t *testing.T) {
	debit, credit := NewLedgerPair("t1", "w1", "w2", 100)
	if debit.Type != EntryDebit || credit.Type != EntryCredit {
		t.Fatalf("unexpected entry types: %s/%s", debit.Type, credit.Type)
	}
	if debit.WalletID != "w1" || credit.WalletID != "w2" {
		t.Fatalf("unexpected wallet ids: %s/%s", debit.WalletID, credit.WalletID)
	}
	if debit.Amount != credit.Amount {
		t.Fatalf("ledger pair must balance: debit %d != credit %d", debit.Amount, credit.Amount)
	}
}
