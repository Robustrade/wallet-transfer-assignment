package domain

import (
	"fmt"
	"time"
)

// Wallet is an account holding a balance in a single currency.
type Wallet struct {
	ID        string
	Balance   Money
	Version   int64
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Debit removes amount from the wallet balance. It returns ErrInsufficientFunds
// if the balance would go negative and ErrCurrencyMismatch on a currency clash.
// The invariant "balance >= 0" is enforced here as well as by a DB CHECK.
func (w *Wallet) Debit(amount Money) error {
	if !amount.IsPositive() {
		return fmt.Errorf("%w: debit amount must be positive", ErrValidation)
	}
	newBalance, err := w.Balance.Sub(amount)
	if err != nil {
		return err
	}
	w.Balance = newBalance
	return nil
}

// Credit adds amount to the wallet balance.
func (w *Wallet) Credit(amount Money) error {
	if !amount.IsPositive() {
		return fmt.Errorf("%w: credit amount must be positive", ErrValidation)
	}
	newBalance, err := w.Balance.Add(amount)
	if err != nil {
		return err
	}
	w.Balance = newBalance
	return nil
}

// CanCover reports whether the wallet can fund the given amount.
func (w *Wallet) CanCover(amount Money) (bool, error) {
	return w.Balance.GreaterThanOrEqual(amount)
}
