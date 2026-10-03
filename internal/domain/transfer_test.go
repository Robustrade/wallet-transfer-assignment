package domain_test

import (
	"errors"
	"testing"

	"github.com/pravinkanna/wallet-transfer-assignment/internal/domain"
)

func TestNewTransferStartsPending(t *testing.T) {
	req := domain.TransferRequest{
		IdempotencyKey: "key-1",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         100,
	}

	got := domain.NewTransfer("transfer-1", req)

	want := domain.Transfer{
		ID:             "transfer-1",
		IdempotencyKey: "key-1",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         100,
		State:          domain.StatePending,
	}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestTransferStateTransitions(t *testing.T) {
	markProcessed := (*domain.Transfer).MarkProcessed
	markFailed := (*domain.Transfer).MarkFailed

	tests := []struct {
		name      string
		from      domain.TransferState
		mark      func(*domain.Transfer) error
		wantState domain.TransferState
		wantErr   error
	}{
		{"PENDING to PROCESSED", domain.StatePending, markProcessed, domain.StateProcessed, nil},
		{"PENDING to FAILED", domain.StatePending, markFailed, domain.StateFailed, nil},
		{"PROCESSED is final: to FAILED", domain.StateProcessed, markFailed, domain.StateProcessed, domain.ErrInvalidTransition},
		{"PROCESSED is final: to PROCESSED", domain.StateProcessed, markProcessed, domain.StateProcessed, domain.ErrInvalidTransition},
		{"FAILED is final: to PROCESSED", domain.StateFailed, markProcessed, domain.StateFailed, domain.ErrInvalidTransition},
		{"FAILED is final: to FAILED", domain.StateFailed, markFailed, domain.StateFailed, domain.ErrInvalidTransition},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transfer := domain.Transfer{State: tt.from}

			err := tt.mark(&transfer)

			if !errors.Is(err, tt.wantErr) {
				t.Errorf("err = %v, want %v", err, tt.wantErr)
			}
			if transfer.State != tt.wantState {
				t.Errorf("state = %s, want %s", transfer.State, tt.wantState)
			}
		})
	}
}
