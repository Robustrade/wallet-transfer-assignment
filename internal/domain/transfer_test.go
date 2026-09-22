package domain

import "testing"

func TestCanTransition(t *testing.T) {
	tests := []struct {
		name string
		from TransferState
		to   TransferState
		want bool
	}{
		{"pending to processed is allowed", TransferPending, TransferProcessed, true},
		{"pending to failed is allowed", TransferPending, TransferFailed, true},
		{"processed to failed is not allowed", TransferProcessed, TransferFailed, false},
		{"failed to processed is not allowed", TransferFailed, TransferProcessed, false},
		{"pending to pending is not allowed", TransferPending, TransferPending, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CanTransition(tt.from, tt.to); got != tt.want {
				t.Errorf("CanTransition(%s, %s) = %v, want %v", tt.from, tt.to, got, tt.want)
			}
		})
	}
}

func TestTransfer_Transition(t *testing.T) {
	xfer := Transfer{State: TransferPending}

	if err := xfer.Transition(TransferProcessed, ""); err != nil {
		t.Fatalf("unexpected error transitioning to processed: %v", err)
	}
	if xfer.State != TransferProcessed {
		t.Fatalf("state = %s, want PROCESSED", xfer.State)
	}

	if err := xfer.Transition(TransferFailed, "too late"); err == nil {
		t.Fatal("expected error transitioning a processed transfer to failed, got nil")
	}
}

func TestWallet_CanDebit(t *testing.T) {
	w := Wallet{Balance: 100}

	if !w.CanDebit(100) {
		t.Error("expected wallet with balance 100 to be able to debit 100")
	}
	if w.CanDebit(101) {
		t.Error("expected wallet with balance 100 to not be able to debit 101")
	}
	if w.CanDebit(0) {
		t.Error("expected CanDebit(0) to be false")
	}
	if w.CanDebit(-5) {
		t.Error("expected CanDebit(negative) to be false")
	}
}
