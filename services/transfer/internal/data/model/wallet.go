// Package model holds the domain entities for the transfer service.
package model

import (
	"time"

	"github.com/Robustrade/wallet-transfer-assignment/common/util/money"
)

// Wallet is the domain entity for an account holding a balance in a single
// currency. Balance is stored as a denormalized read view of the ledger.
type Wallet struct {
	ID        string
	Balance   money.Money
	Currency  string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Wallets is a typed collection of Wallet entities.
type Wallets []Wallet

// Pick returns the (from, to) wallets matching the supplied ids. ok is true
// only when both ids are found. The model layer reports the boolean outcome;
// callers translate it to whatever transport-level error is appropriate.
func (ws Wallets) Pick(fromID, toID string) (from, to Wallet, ok bool) {
	var fromOK, toOK bool
	for _, w := range ws {
		switch w.ID {
		case fromID:
			from, fromOK = w, true
		case toID:
			to, toOK = w, true
		}
	}
	return from, to, fromOK && toOK
}
