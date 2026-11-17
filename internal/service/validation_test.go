package service

import (
	"errors"
	"testing"
)

func TestValidateTransfer(t *testing.T) {
	tests := []struct {
		name         string
		fromWalletID string
		toWalletID   string
		amount       int64
		expectedErr  error
	}{
		{
			name:         "valid transfer",
			fromWalletID: "wallet_1",
			toWalletID:   "wallet_2",
			amount:       100,
			expectedErr:  nil,
		},
		{
			name:         "zero amount",
			fromWalletID: "wallet_1",
			toWalletID:   "wallet_2",
			amount:       0,
			expectedErr:  ErrInvalidAmount,
		},
		{
			name:         "negative amount",
			fromWalletID: "wallet_1",
			toWalletID:   "wallet_2",
			amount:       -100,
			expectedErr:  ErrInvalidAmount,
		},
		{
			name:         "missing wallet ID",
			fromWalletID: "",
			toWalletID:   "wallet_2",
			amount:       100,
			expectedErr:  ErrInvalidWallet,
		},
		{
			name:         "same wallet",
			fromWalletID: "wallet_1",
			toWalletID:   "wallet_1",
			amount:       100,
			expectedErr:  ErrSameWallet,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateTransfer(
				tt.fromWalletID,
				tt.toWalletID,
				tt.amount,
			)

			if !errors.Is(err, tt.expectedErr) {
				t.Errorf(
					"expected error %v, got %v",
					tt.expectedErr,
					err,
				)
			}
		})
	}
}
