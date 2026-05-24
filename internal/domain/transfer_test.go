package domain_test

import (
	"errors"
	"testing"

	"wallet-transfer-assignment/internal/domain"
)

func TestTransferValidate(t *testing.T) {
	tests := []struct {
		name     string
		transfer domain.Transfer
		wantErr  error
	}{
		{
			name: "invalid amount",
			transfer: domain.Transfer{
				FromWalletID: "wallet_1",
				ToWalletID:   "wallet_2",
				Amount:       0,
			},
			wantErr: domain.ErrInvalidAmount,
		},
		{
			name: "same wallet transfer",
			transfer: domain.Transfer{
				FromWalletID: "wallet_1",
				ToWalletID:   "wallet_1",
				Amount:       100,
			},
			wantErr: domain.ErrSameWalletTransfer,
		},
		{
			name: "valid transfer",
			transfer: domain.Transfer{
				FromWalletID: "wallet_1",
				ToWalletID:   "wallet_2",
				Amount:       100,
			},
			wantErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.transfer.Validate()
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("expected error %v, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestTransferMarkProcessed(t *testing.T) {
	transfer := domain.Transfer{Status: domain.TransferStatusPending}

	err := transfer.MarkProcessed()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if transfer.Status != domain.TransferStatusProcessed {
		t.Fatalf("expected status %q, got %q", domain.TransferStatusProcessed, transfer.Status)
	}
}

func TestTransferMarkProcessedRejectsNonPendingTransfer(t *testing.T) {
	transfer := domain.Transfer{Status: domain.TransferStatusFailed}

	err := transfer.MarkProcessed()
	if !errors.Is(err, domain.ErrInvalidTransferState) {
		t.Fatalf("expected error %v, got %v", domain.ErrInvalidTransferState, err)
	}
}

func TestTransferMarkFailed(t *testing.T) {
	transfer := domain.Transfer{Status: domain.TransferStatusPending}

	err := transfer.MarkFailed()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if transfer.Status != domain.TransferStatusFailed {
		t.Fatalf("expected status %q, got %q", domain.TransferStatusFailed, transfer.Status)
	}
}

func TestTransferMarkFailedRejectsNonPendingTransfer(t *testing.T) {
	transfer := domain.Transfer{Status: domain.TransferStatusProcessed}

	err := transfer.MarkFailed()
	if !errors.Is(err, domain.ErrInvalidTransferState) {
		t.Fatalf("expected error %v, got %v", domain.ErrInvalidTransferState, err)
	}
}
