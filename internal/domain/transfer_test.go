package domain

import (
	"errors"
	"testing"
)

func TestTransfer_CanTransitionTo(t *testing.T) {
	tests := []struct {
		name    string
		from    TransferStatus
		to      TransferStatus
		allowed bool
	}{
		{"pending to processed", StatusPending, StatusProcessed, true},
		{"pending to failed", StatusPending, StatusFailed, true},
		{"pending to pending", StatusPending, StatusPending, false},
		{"processed to failed", StatusProcessed, StatusFailed, false},
		{"processed to processed", StatusProcessed, StatusProcessed, false},
		{"failed to processed", StatusFailed, StatusProcessed, false},
		{"failed to pending", StatusFailed, StatusPending, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr := Transfer{Status: tt.from}
			got := tr.CanTransitionTo(tt.to)
			if got != tt.allowed {
				t.Errorf("CanTransitionTo(%s->%s) = %v, want %v",
					tt.from, tt.to, got, tt.allowed)
			}
		})
	}
}

func TestTransfer_TransitionTo(t *testing.T) {
	t.Run("valid transition updates status", func(t *testing.T) {
		tr := Transfer{Status: StatusPending}
		if err := tr.TransitionTo(StatusProcessed); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if tr.Status != StatusProcessed {
			t.Errorf("status = %s, want %s", tr.Status, StatusProcessed)
		}
	})

	t.Run("invalid transition returns error and keeps status", func(t *testing.T) {
		tr := Transfer{Status: StatusProcessed}
		err := tr.TransitionTo(StatusFailed)
		if !errors.Is(err, ErrInvalidStateTransition) {
			t.Fatalf("error = %v, want ErrInvalidStateTransition", err)
		}
		if tr.Status != StatusProcessed {
			t.Errorf("status mutated to %s, want %s", tr.Status, StatusProcessed)
		}
	})
}

func TestValidateTransferRequest(t *testing.T) {
	tests := []struct {
		name    string
		from    string
		to      string
		amount  int64
		wantErr error
	}{
		{"valid", "wallet_1", "wallet_2", 100, nil},
		{"zero amount", "wallet_1", "wallet_2", 0, ErrInvalidAmount},
		{"negative amount", "wallet_1", "wallet_2", -50, ErrInvalidAmount},
		{"same wallet", "wallet_1", "wallet_1", 100, ErrSameWallet},
		{"empty from", "", "wallet_2", 100, ErrMissingWallet},
		{"empty to", "wallet_1", "", 100, ErrMissingWallet},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateTransferRequest(tt.from, tt.to, tt.amount)
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("ValidateTransferRequest() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}