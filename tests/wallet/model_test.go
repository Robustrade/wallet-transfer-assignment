package wallet_test

import (
	"testing"

	"github.com/yashbaheti1971/wallet-transfer-assignment/internal/domain/wallet"
)

func TestWallet_ValidateForCreate(t *testing.T) {
	tests := []struct {
		name    string
		wallet  wallet.Wallet
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid wallet",
			wallet: wallet.Wallet{
				ID:       "wallet_123",
				OwnerID:  "user_123",
				Currency: "USD",
				Status:   wallet.StatusActive,
			},
			wantErr: false,
		},
		{
			name: "missing id",
			wallet: wallet.Wallet{
				OwnerID:  "user_123",
				Currency: "USD",
				Status:   wallet.StatusActive,
			},
			wantErr: true,
			errMsg:  "wallet: id is required",
		},
		{
			name: "missing owner_id",
			wallet: wallet.Wallet{
				ID:       "wallet_123",
				Currency: "USD",
				Status:   wallet.StatusActive,
			},
			wantErr: true,
			errMsg:  "wallet: owner_id is required",
		},
		{
			name: "missing currency",
			wallet: wallet.Wallet{
				ID:      "wallet_123",
				OwnerID: "user_123",
				Status:  wallet.StatusActive,
			},
			wantErr: true,
			errMsg:  "wallet: currency is required",
		},
		{
			name: "invalid status",
			wallet: wallet.Wallet{
				ID:       "wallet_123",
				OwnerID:  "user_123",
				Currency: "USD",
				Status:   "UNKNOWN",
			},
			wantErr: true,
			errMsg:  "wallet: invalid status",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.wallet.ValidateForCreate()
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateForCreate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err != nil && err.Error() != tt.errMsg {
				t.Errorf("ValidateForCreate() error msg = %v, want %v", err.Error(), tt.errMsg)
			}
		})
	}
}

func TestWallet_IsActive(t *testing.T) {
	wActive := wallet.Wallet{Status: wallet.StatusActive}
	wInactive := wallet.Wallet{Status: wallet.StatusInactive}

	if !wActive.IsActive() {
		t.Error("Expected IsActive to be true for StatusActive")
	}
	if wInactive.IsActive() {
		t.Error("Expected IsActive to be false for StatusInactive")
	}
}
