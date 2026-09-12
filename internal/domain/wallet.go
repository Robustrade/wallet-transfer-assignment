package domain

import (
	"math"
	"time"
)

// SystemTreasuryWalletID is the reserved identifier for the system treasury account.
const SystemTreasuryWalletID = "system_treasury"

// Wallet represents an individual account holding funds.
type Wallet struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Balance   int64     `json:"balance"` // Stored in minor currency units (e.g. cents)
	Currency  string    `json:"currency"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// CanDebit returns true if the wallet has sufficient funds to cover the requested amount and the amount is positive.
func (w *Wallet) CanDebit(amount int64) bool {
	if amount <= 0 {
		return false
	}
	return w.Balance >= amount
}

// CanCredit returns true if adding amount to the wallet balance will not overflow int64.
func (w *Wallet) CanCredit(amount int64) bool {
	if amount <= 0 {
		return false
	}
	return w.Balance <= math.MaxInt64-amount
}

// Debit deducts funds from the wallet balance.
func (w *Wallet) Debit(amount int64) error {
	if amount <= 0 {
		return ErrInvalidAmount
	}
	if !w.CanDebit(amount) {
		return ErrInsufficientFunds
	}
	w.Balance -= amount
	w.UpdatedAt = time.Now().UTC()
	return nil
}

// Credit adds funds to the wallet balance, returning ErrBalanceOverflow if int64 capacity is exceeded.
func (w *Wallet) Credit(amount int64) error {
	if amount <= 0 {
		return ErrInvalidAmount
	}
	if !w.CanCredit(amount) {
		return ErrBalanceOverflow
	}
	w.Balance += amount
	w.UpdatedAt = time.Now().UTC()
	return nil
}
