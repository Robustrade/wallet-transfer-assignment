package models

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestTransfer_Validate(t *testing.T) {
	tests := []struct {
		name        string
		transfer    Transfer
		expectError bool
		errorMsg    string
	}{
		{
			name: "valid transfer",
			transfer: Transfer{
				FromWalletID: "wallet-1",
				ToWalletID:   "wallet-2",
				Amount:       100,
			},
			expectError: false,
		},
		{
			name: "zero amount",
			transfer: Transfer{
				FromWalletID: "wallet-1",
				ToWalletID:   "wallet-2",
				Amount:       0,
			},
			expectError: true,
			errorMsg:    "amount must be positive",
		},
		{
			name: "negative amount",
			transfer: Transfer{
				FromWalletID: "wallet-1",
				ToWalletID:   "wallet-2",
				Amount:       -50,
			},
			expectError: true,
			errorMsg:    "amount must be positive",
		},
		{
			name: "missing from wallet",
			transfer: Transfer{
				FromWalletID: "",
				ToWalletID:   "wallet-2",
				Amount:       100,
			},
			expectError: true,
			errorMsg:    "both wallet IDs must be provided",
		},
		{
			name: "missing to wallet",
			transfer: Transfer{
				FromWalletID: "wallet-1",
				ToWalletID:   "",
				Amount:       100,
			},
			expectError: true,
			errorMsg:    "both wallet IDs must be provided",
		},
		{
			name: "same wallet",
			transfer: Transfer{
				FromWalletID: "wallet-1",
				ToWalletID:   "wallet-1",
				Amount:       100,
			},
			expectError: true,
			errorMsg:    "cannot transfer to the same wallet",
		},
		{
			name: "both wallets missing",
			transfer: Transfer{
				FromWalletID: "",
				ToWalletID:   "",
				Amount:       100,
			},
			expectError: true,
			errorMsg:    "both wallet IDs must be provided",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.transfer.Validate()
			if tt.expectError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestIsValidTransition(t *testing.T) {
	tests := []struct {
		name          string
		currentStatus string
		newStatus     string
		expected      bool
	}{
		{
			name:          "pending to processed",
			currentStatus: TransferStatusPending,
			newStatus:     TransferStatusProcessed,
			expected:      true,
		},
		{
			name:          "pending to failed",
			currentStatus: TransferStatusPending,
			newStatus:     TransferStatusFailed,
			expected:      true,
		},
		{
			name:          "processed to pending",
			currentStatus: TransferStatusProcessed,
			newStatus:     TransferStatusPending,
			expected:      false,
		},
		{
			name:          "processed to failed",
			currentStatus: TransferStatusProcessed,
			newStatus:     TransferStatusFailed,
			expected:      false,
		},
		{
			name:          "failed to pending",
			currentStatus: TransferStatusFailed,
			newStatus:     TransferStatusPending,
			expected:      false,
		},
		{
			name:          "failed to processed",
			currentStatus: TransferStatusFailed,
			newStatus:     TransferStatusProcessed,
			expected:      false,
		},
		{
			name:          "pending to pending",
			currentStatus: TransferStatusPending,
			newStatus:     TransferStatusPending,
			expected:      false,
		},
		{
			name:          "unknown status",
			currentStatus: "UNKNOWN",
			newStatus:     TransferStatusProcessed,
			expected:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsValidTransition(tt.currentStatus, tt.newStatus)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestTransfer_TableName(t *testing.T) {
	transfer := Transfer{}
	assert.Equal(t, "transfers", transfer.TableName())
}

func TestTransfer_Fields(t *testing.T) {
	now := time.Now()
	transfer := Transfer{
		TransferID:   "transfer-123",
		FromWalletID: "wallet-1",
		ToWalletID:   "wallet-2",
		Amount:       500,
		Status:       TransferStatusPending,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	assert.Equal(t, "transfer-123", transfer.TransferID)
	assert.Equal(t, "wallet-1", transfer.FromWalletID)
	assert.Equal(t, "wallet-2", transfer.ToWalletID)
	assert.Equal(t, int64(500), transfer.Amount)
	assert.Equal(t, TransferStatusPending, transfer.Status)
}

func TestTransferStatusConstants(t *testing.T) {
	assert.Equal(t, "PENDING", TransferStatusPending)
	assert.Equal(t, "PROCESSED", TransferStatusProcessed)
	assert.Equal(t, "FAILED", TransferStatusFailed)
}
