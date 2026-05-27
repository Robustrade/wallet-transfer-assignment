package models

import (
	"testing"
)

func TestTransfer_Validate(t *testing.T) {
	tests := []struct {
		name     string
		transfer *Transfer
		wantErr  bool
	}{
		{
			name: "valid transfer",
			transfer: &Transfer{
				ID:           "transfer_1",
				FromWalletID: "wallet_1",
				ToWalletID:   "wallet_2",
				Amount:       100.0,
				Status:       TransferStatusPending,
			},
			wantErr: false,
		},
		{
			name: "same wallet",
			transfer: &Transfer{
				ID:           "transfer_1",
				FromWalletID: "wallet_1",
				ToWalletID:   "wallet_1",
				Amount:       100.0,
				Status:       TransferStatusPending,
			},
			wantErr: true,
		},
		{
			name: "negative amount",
			transfer: &Transfer{
				ID:           "transfer_1",
				FromWalletID: "wallet_1",
				ToWalletID:   "wallet_2",
				Amount:       -10.0,
				Status:       TransferStatusPending,
			},
			wantErr: true,
		},
		{
			name: "invalid status",
			transfer: &Transfer{
				ID:           "transfer_1",
				FromWalletID: "wallet_1",
				ToWalletID:   "wallet_2",
				Amount:       100.0,
				Status:       "INVALID",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.transfer.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Transfer.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestTransfer_StateTransitions(t *testing.T) {
	t.Run("mark as processed", func(t *testing.T) {
		transfer := &Transfer{Status: TransferStatusPending}
		err := transfer.MarkAsProcessed()
		if err != nil {
			t.Errorf("MarkAsProcessed() error = %v", err)
		}
		if transfer.Status != TransferStatusProcessed {
			t.Errorf("Status = %v, want %v", transfer.Status, TransferStatusProcessed)
		}
	})

	t.Run("cannot mark processed as processed", func(t *testing.T) {
		transfer := &Transfer{Status: TransferStatusProcessed}
		err := transfer.MarkAsProcessed()
		if err == nil {
			t.Error("MarkAsProcessed() expected error, got nil")
		}
	})

	t.Run("mark as failed", func(t *testing.T) {
		transfer := &Transfer{Status: TransferStatusPending}
		err := transfer.MarkAsFailed()
		if err != nil {
			t.Errorf("MarkAsFailed() error = %v", err)
		}
		if transfer.Status != TransferStatusFailed {
			t.Errorf("Status = %v, want %v", transfer.Status, TransferStatusFailed)
		}
	})

	t.Run("cannot mark processed as failed", func(t *testing.T) {
		transfer := &Transfer{Status: TransferStatusProcessed}
		err := transfer.MarkAsFailed()
		if err == nil {
			t.Error("MarkAsFailed() expected error, got nil")
		}
	})
}
