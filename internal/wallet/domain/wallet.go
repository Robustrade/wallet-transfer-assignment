package domain

import (
	"time"
)

// Wallet represents a user's wallet
type Wallet struct {
	ID        string
	UserID    string
	Balance   int64 // Amount in cents
	Currency  string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// NewWallet creates a new wallet
func NewWallet(id string, userID string, currency string) *Wallet {
	return &Wallet{
		ID:        id,
		UserID:    userID,
		Balance:   0,
		Currency:  currency,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
}

// CanDebit checks if wallet has sufficient balance
func (w *Wallet) CanDebit(amount int64) bool {
	return w.Balance >= amount
}

// Debit deducts amount from wallet balance
func (w *Wallet) Debit(amount int64) error {
	if amount <= 0 {
		return ErrInvalidAmount
	}
	if !w.CanDebit(amount) {
		return ErrInsufficientFunds
	}
	w.Balance -= amount
	w.UpdatedAt = time.Now()
	return nil
}

// Credit adds amount to wallet balance
func (w *Wallet) Credit(amount int64) error {
	if amount <= 0 {
		return ErrInvalidAmount
	}
	w.Balance += amount
	w.UpdatedAt = time.Now()
	return nil
}
