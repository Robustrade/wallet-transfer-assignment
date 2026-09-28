package domain

import (
	"fmt"
	"math"
	"time"
)

// Wallet holds a balance in minor units (e.g. cents) of a single currency.
type Wallet struct {
	ID        string
	Balance   int64
	Currency  string
	UpdatedAt time.Time
}

// Debit and Credit are the single-wallet operations, and the canonical
// statement of each rule. Transfers use ApplyTransfer instead, which runs the
// same checks (checkDebit, checkCredit) on BOTH wallets before changing
// either, so a two-wallet move is all-or-nothing. Calling Debit then Credit
// would leave the source debited in memory if the credit failed.

// Debit removes amount from the wallet. Spending the whole balance is
// allowed; going below zero is not. On error the balance is unchanged.
func (w *Wallet) Debit(amount int64) error {
	if err := w.checkDebit(amount); err != nil {
		return err
	}
	w.Balance -= amount
	return nil
}

// Credit adds amount to the wallet. On error the balance is unchanged.
func (w *Wallet) Credit(amount int64) error {
	if err := w.checkCredit(amount); err != nil {
		return err
	}
	w.Balance += amount
	return nil
}

func (w *Wallet) checkDebit(amount int64) error {
	if amount <= 0 {
		return fmt.Errorf("%w: amount must be positive, got %d", ErrValidation, amount)
	}
	if w.Balance < amount {
		return fmt.Errorf("wallet %s: %w", w.ID, ErrInsufficientFunds)
	}
	return nil
}

func (w *Wallet) checkCredit(amount int64) error {
	if amount <= 0 {
		return fmt.Errorf("%w: amount must be positive, got %d", ErrValidation, amount)
	}
	// Written as a subtraction so the check itself cannot overflow:
	// Balance + amount > MaxInt64  <=>  Balance > MaxInt64 - amount.
	if w.Balance > math.MaxInt64-amount {
		return fmt.Errorf("wallet %s: %w", w.ID, ErrBalanceOverflow)
	}
	return nil
}

// ApplyTransfer moves amount from one wallet to the other.
//
// It checks every rule before changing anything, so on error BOTH wallets
// are unchanged. Calling from.Debit and then to.Credit directly would leave
// the source debited in memory if the credit then failed.
//
// Rules are checked in a fixed order, and the first one that fails decides
// the failure reason: currency match, then sufficient funds, then
// destination overflow.
func ApplyTransfer(from, to *Wallet, amount int64) error {
	if from.ID == to.ID {
		return fmt.Errorf("%w: cannot transfer to the same wallet", ErrValidation)
	}
	if from.Currency != to.Currency {
		return fmt.Errorf("%s (%s) -> %s (%s): %w",
			from.ID, from.Currency, to.ID, to.Currency, ErrCurrencyMismatch)
	}
	if err := from.checkDebit(amount); err != nil {
		return err
	}
	if err := to.checkCredit(amount); err != nil {
		return err
	}

	// Both checks passed, so neither call below can fail.
	from.Balance -= amount
	to.Balance += amount
	return nil
}
