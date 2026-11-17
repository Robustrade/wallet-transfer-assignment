package model

import (
	"math"
	"time"

	helpers "github.com/Robustrade/wallet-transfer-assignment/helpers"
)

// Wallet is a stored-balance account.
// OpeningBalance is set at create and is immutable afterward (DB trigger + no service mutator).
type Wallet struct {
	ID             string
	OpeningBalance helpers.Money
	Balance        helpers.Money
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// NewWallet creates a wallet with opening_balance = balance.
// id must be non-empty after trim; balance must be >= 0.
func NewWallet(id string, balance int64) (*Wallet, error) {
	id, err := NormalizeID(id)
	if err != nil {
		return nil, err
	}
	bal, err := NewBalance(balance)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	return &Wallet{
		ID:             id,
		OpeningBalance: bal,
		Balance:        bal,
		CreatedAt:      now,
		UpdatedAt:      now,
	}, nil
}

// CanDebit reports whether the wallet holds at least amount.
func (w *Wallet) CanDebit(amount helpers.Money) bool {
	return w.Balance >= amount
}

// CanCredit reports whether amount can be added without overflowing int64.
// Checks headroom before any mutation: balance + amount must stay <= MaxInt64.
func (w *Wallet) CanCredit(amount helpers.Money) bool {
	if amount <= 0 {
		return false
	}
	return w.Balance <= helpers.Money(math.MaxInt64)-amount
}
