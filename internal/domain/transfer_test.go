package domain_test

import (
	"errors"
	"testing"

	"github.com/gaurav2204bits-collab/wallet-transfer-assignment/internal/domain"
)

func newPendingTransfer(t *testing.T) *domain.Transfer {
	t.Helper()
	tr, err := domain.NewTransfer("txn_1", "key_1", "wallet_1", "wallet_2", usd(100))
	if err != nil {
		t.Fatalf("NewTransfer: %v", err)
	}
	return tr
}

func TestNewTransferValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		id, key, from, to string
		amount            domain.Money
		wantErr           bool
	}{
		{"valid", "txn", "k", "a", "b", usd(1), false},
		{"missing id", "", "k", "a", "b", usd(1), true},
		{"missing key", "txn", "", "a", "b", usd(1), true},
		{"self transfer", "txn", "k", "a", "a", usd(1), true},
		{"zero amount", "txn", "k", "a", "b", usd(0), true},
		{"negative amount", "txn", "k", "a", "b", domain.Money{Amount: -5, Currency: "USD"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := domain.NewTransfer(tt.id, tt.key, tt.from, tt.to, tt.amount)
			if tt.wantErr && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestTransferStateMachine(t *testing.T) {
	t.Parallel()

	t.Run("PENDING -> PROCESSED is allowed", func(t *testing.T) {
		t.Parallel()
		tr := newPendingTransfer(t)
		if err := tr.MarkProcessed(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if tr.State != domain.TransferProcessed {
			t.Fatalf("state = %s, want PROCESSED", tr.State)
		}
	})

	t.Run("PENDING -> FAILED is allowed and records reason", func(t *testing.T) {
		t.Parallel()
		tr := newPendingTransfer(t)
		if err := tr.MarkFailed("insufficient funds"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if tr.State != domain.TransferFailed || tr.FailureReason != "insufficient funds" {
			t.Fatalf("got state=%s reason=%q", tr.State, tr.FailureReason)
		}
	})

	t.Run("PROCESSED is terminal", func(t *testing.T) {
		t.Parallel()
		tr := newPendingTransfer(t)
		_ = tr.MarkProcessed()
		err := tr.MarkFailed("late failure")
		if !errors.Is(err, domain.ErrInvalidStateTransition) {
			t.Fatalf("got %v, want ErrInvalidStateTransition", err)
		}
		if tr.State != domain.TransferProcessed {
			t.Fatalf("state mutated to %s", tr.State)
		}
	})

	t.Run("double PROCESSED is rejected", func(t *testing.T) {
		t.Parallel()
		tr := newPendingTransfer(t)
		_ = tr.MarkProcessed()
		if err := tr.MarkProcessed(); !errors.Is(err, domain.ErrInvalidStateTransition) {
			t.Fatalf("got %v, want ErrInvalidStateTransition", err)
		}
	})
}
