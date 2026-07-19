package domain

import (
	"errors"
	"testing"
)

func TestNewMoneyDefaultsCurrency(t *testing.T) {
	money, err := NewMoney(100, "")
	if err != nil {
		t.Fatalf("NewMoney returned error: %v", err)
	}
	if money.Currency != DefaultCurrency {
		t.Fatalf("currency = %q, want %q", money.Currency, DefaultCurrency)
	}
}

func TestNewMoneyRejectsInvalidAmount(t *testing.T) {
	_, err := NewMoney(0, "USD")
	if !errors.Is(err, ErrInvalidAmount) {
		t.Fatalf("err = %v, want %v", err, ErrInvalidAmount)
	}
}

func TestNewMoneyRejectsInvalidCurrency(t *testing.T) {
	_, err := NewMoney(100, "US")
	if !errors.Is(err, ErrInvalidCurrency) {
		t.Fatalf("err = %v, want %v", err, ErrInvalidCurrency)
	}
}
