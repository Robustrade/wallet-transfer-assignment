package domain

import (
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// Wallet holds the current balance. Balance is exact decimal money, not a float.
type Wallet struct {
	ID        uuid.UUID
	Balance   decimal.Decimal
	IsActive  bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

// CanDebit reports whether amount can be taken out. This is an early check; the
// database CHECK(balance >= 0) under a lock is what actually decides.
func (w Wallet) CanDebit(amount decimal.Decimal) error {
	if !w.IsActive {
		return ErrWalletInactive
	}
	if w.Balance.LessThan(amount) {
		return ErrInsufficientFunds
	}
	return nil
}

// CanReceive reports whether the wallet may be credited.
func (w Wallet) CanReceive() error {
	if !w.IsActive {
		return ErrWalletInactive
	}
	return nil
}
