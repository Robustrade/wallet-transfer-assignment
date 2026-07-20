package domain

import (
	"errors"
	"testing"
)

func TestWalletCanDebit(t *testing.T) {
	money, err := NewMoney(100, "USD")
	if err != nil {
		t.Fatalf("NewMoney returned error: %v", err)
	}
	wallet := Wallet{ID: "wallet-a", Currency: "USD", Balance: 100, Status: WalletStatusActive}

	if err := wallet.CanDebit(money); err != nil {
		t.Fatalf("CanDebit returned error: %v", err)
	}
}

func TestWalletCanDebitRejectsInsufficientFunds(t *testing.T) {
	money, err := NewMoney(101, "USD")
	if err != nil {
		t.Fatalf("NewMoney returned error: %v", err)
	}
	wallet := Wallet{ID: "wallet-a", Currency: "USD", Balance: 100, Status: WalletStatusActive}

	if err := wallet.CanDebit(money); !errors.Is(err, ErrInsufficientFunds) {
		t.Fatalf("err = %v, want %v", err, ErrInsufficientFunds)
	}
}

func TestWalletCanCreditRejectsInactiveWallet(t *testing.T) {
	wallet := Wallet{ID: "wallet-a", Currency: "USD", Balance: 100, Status: WalletStatusFrozen}

	if err := wallet.CanCredit("USD"); !errors.Is(err, ErrWalletNotActive) {
		t.Fatalf("err = %v, want %v", err, ErrWalletNotActive)
	}
}
