package models

import (
	"errors"
	"time"
)

// Wallet represents a wallet entity
type Wallet struct {
	ID        string
	Balance   float64
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Validate validates wallet data
func (w *Wallet) Validate() error {
	if w.ID == "" {
		return errors.New("wallet ID cannot be empty")
	}
	if w.Balance < 0 {
		return errors.New("wallet balance cannot be negative")
	}
	return nil
}

// HasSufficientBalance checks if wallet has enough balance for a transfer
func (w *Wallet) HasSufficientBalance(amount float64) bool {
	return w.Balance >= amount
}

// Debit reduces the wallet balance
func (w *Wallet) Debit(amount float64) error {
	if amount <= 0 {
		return errors.New("debit amount must be positive")
	}
	if !w.HasSufficientBalance(amount) {
		return errors.New("insufficient balance")
	}
	w.Balance -= amount
	w.UpdatedAt = time.Now()
	return nil
}

// Credit increases the wallet balance
func (w *Wallet) Credit(amount float64) error {
	if amount <= 0 {
		return errors.New("credit amount must be positive")
	}
	w.Balance += amount
	w.UpdatedAt = time.Now()
	return nil
}
