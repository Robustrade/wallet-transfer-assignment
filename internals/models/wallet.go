package models

import (
	"errors"

	"github.com/google/uuid"
)

var ErrInsufficientBalance = errors.New("insufficient balance")

// Wallet is the domain representation of a wallet.
// Persistence concerns (columns, SQL) live in the repository layer, not here.
type Wallet struct {
	ID        uuid.UUID
	OwnerName string
	Balance   int64 // minor units (e.g. cents) to avoid float precision issues
	Version   int32
}

// Debit reduces the wallet balance, enforcing the non-negative invariant.
// This is domain logic — it belongs here, not scattered in SQL or service code.
func (w *Wallet) Debit(amount int64) error {
	if amount <= 0 {
		return errors.New("debit amount must be positive")
	}
	if w.Balance < amount {
		return ErrInsufficientBalance
	}
	w.Balance -= amount
	return nil
}

// Credit increases the wallet balance.
func (w *Wallet) Credit(amount int64) error {
	if amount <= 0 {
		return errors.New("credit amount must be positive")
	}
	w.Balance += amount
	return nil
}
