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
				Balance: 100.0,
			},
			wantErr: false,
		},
		{
			name: "empty ID",
			wallet: &Wallet{
				ID:      "",
				Balance: 100.0,
			},
			wantErr: true,
		},
		{
			name: "negative balance",
			wallet: &Wallet{
				ID:      "wallet_1",
				Balance: -10.0,
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
	wallet := &Wallet{ID: "wallet_1", Balance: 100.0}

	tests := []struct {
		name   string
		amount float64
		want   bool
	}{
		{"sufficient", 50.0, true},
		{"exact", 100.0, true},
		{"insufficient", 150.0, false},
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
		initialBalance float64
		amount         float64
		wantErr        bool
		wantBalance    float64
	}{
		{
			name:           "successful debit",
			initialBalance: 100.0,
			amount:         30.0,
			wantErr:        false,
			wantBalance:    70.0,
		},
		{
			name:           "insufficient balance",
			initialBalance: 50.0,
			amount:         100.0,
			wantErr:        true,
			wantBalance:    50.0,
		},
		{
			name:           "negative amount",
			initialBalance: 100.0,
			amount:         -10.0,
			wantErr:        true,
			wantBalance:    100.0,
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
		initialBalance float64
		amount         float64
		wantErr        bool
		wantBalance    float64
	}{
		{
			name:           "successful credit",
			initialBalance: 100.0,
			amount:         50.0,
			wantErr:        false,
			wantBalance:    150.0,
		},
		{
			name:           "negative amount",
			initialBalance: 100.0,
			amount:         -10.0,
			wantErr:        true,
			wantBalance:    100.0,
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
