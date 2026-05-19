package domain

import (
	"time"

	"github.com/shopspring/decimal"
)

// Wallet is an account holding a cached running balance. The balance is kept
// in sync with the ledger inside the same transaction as every ledger write,
// so it is always reconcilable against the sum of the wallet's ledger entries.
type Wallet struct {
	ID        string
	Name      string
	Balance   decimal.Decimal
	CreatedAt time.Time
}

// HasSufficientFunds reports whether the wallet can cover amount.
func (w Wallet) HasSufficientFunds(amount decimal.Decimal) bool {
	return w.Balance.GreaterThanOrEqual(amount)
}
