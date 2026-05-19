package domain

import "testing"

func TestValidateTransferRequest(t *testing.T) {
	t.Run("same wallet", func(t *testing.T) {
		err := ValidateTransferRequest("w1", "w1", 10)
		if err != ErrSameWallet {
			t.Fatalf("got %v, want ErrSameWallet", err)
		}
	})

	t.Run("non positive amount", func(t *testing.T) {
		err := ValidateTransferRequest("w1", "w2", 0)
		if err != ErrInvalidTransfer {
			t.Fatalf("got %v, want ErrInvalidTransfer", err)
		}
	})

	t.Run("valid", func(t *testing.T) {
		if err := ValidateTransferRequest("w1", "w2", 10); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestTransferState_CanTransitionTo(t *testing.T) {
	tests := []struct {
		from TransferState
		to   TransferState
		want bool
	}{
		{TransferStatePending, TransferStateProcessed, true},
		{TransferStatePending, TransferStateFailed, true},
		{TransferStatePending, TransferStatePending, false},
		{TransferStateProcessed, TransferStateFailed, false},
		{TransferStateFailed, TransferStateProcessed, false},
	}

	for _, tc := range tests {
		if got := tc.from.CanTransitionTo(tc.to); got != tc.want {
			t.Fatalf("%s -> %s = %v, want %v", tc.from, tc.to, got, tc.want)
		}
	}
}
