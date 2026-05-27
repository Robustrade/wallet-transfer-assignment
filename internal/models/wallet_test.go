package models

import (
	"testing"
)

func TestWallet_Validate(t *testing.T) {
	tests := []struct {
		name    string
		wallet  *Wallet
		wantErr bool
	}{
		{
			name: "valid wallet",
			wallet: &Wallet{
				ID:      "wallet_1",
				Balance: 10000,
			},
			wantErr: false,
		},
		{
			name: "empty ID",
			wallet: &Wallet{
				ID:      "",
				Balance: 10000,
			},
			wantErr: true,
		},
		{
			name: "negative balance",
			wallet: &Wallet{
				ID:      "wallet_1",
				Balance: -1000,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.wallet.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Wallet.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestWallet_HasSufficientBalance(t *testing.T) {
	wallet := &Wallet{ID: "wallet_1", Balance: 10000}

	tests := []struct {
		name   string
		amount int64
		want   bool
	}{
		{"sufficient", 5000, true},
		{"exact", 10000, true},
		{"insufficient", 15000, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := wallet.HasSufficientBalance(tt.amount); got != tt.want {
				t.Errorf("Wallet.HasSufficientBalance() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestWallet_Debit(t *testing.T) {
	tests := []struct {
		name           string
		initialBalance int64
		amount         int64
		wantErr        bool
		wantBalance    int64
	}{
		{
			name:           "successful debit",
			initialBalance: 10000,
			amount:         3000,
			wantErr:        false,
			wantBalance:    7000,
		},
		{
			name:           "insufficient balance",
			initialBalance: 5000,
			amount:         10000,
			wantErr:        true,
			wantBalance:    5000,
		},
		{
			name:           "negative amount",
			initialBalance: 10000,
			amount:         -1000,
			wantErr:        true,
			wantBalance:    10000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wallet := &Wallet{ID: "wallet_1", Balance: tt.initialBalance}
			err := wallet.Debit(tt.amount)

			if (err != nil) != tt.wantErr {
				t.Errorf("Wallet.Debit() error = %v, wantErr %v", err, tt.wantErr)
			}

			if wallet.Balance != tt.wantBalance {
				t.Errorf("Wallet.Balance = %v, want %v", wallet.Balance, tt.wantBalance)
			}
		})
	}
}

func TestWallet_Credit(t *testing.T) {
	tests := []struct {
		name           string
		initialBalance int64
		amount         int64
		wantErr        bool
		wantBalance    int64
	}{
		{
			name:           "successful credit",
			initialBalance: 10000,
			amount:         5000,
			wantErr:        false,
			wantBalance:    15000,
		},
		{
			name:           "negative amount",
			initialBalance: 10000,
			amount:         -1000,
			wantErr:        true,
			wantBalance:    10000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wallet := &Wallet{ID: "wallet_1", Balance: tt.initialBalance}
			err := wallet.Credit(tt.amount)

			if (err != nil) != tt.wantErr {
				t.Errorf("Wallet.Credit() error = %v, wantErr %v", err, tt.wantErr)
			}

			if wallet.Balance != tt.wantBalance {
				t.Errorf("Wallet.Balance = %v, want %v", wallet.Balance, tt.wantBalance)
			}
		})
	}
}
