package domain

import "time"

type Wallet struct {
	ID        string
	Balance   int64
	CreatedAt time.Time
	UpdatedAt time.Time
}

// CanDebit checks whether the wallet has sufficient balance for the given amount.
// Pure check — does not mutate state or touch the database.
func (w *Wallet) CanDebit(amount int64) error {
	if amount <= 0 {
		return ErrInvalidAmount
	}
	if w.Balance < amount {
		return ErrInsufficientBalance
	}
	return nil
}

// Debit reduces the wallet balance by amount, after validating sufficient funds.
func (w *Wallet) Debit(amount int64) error {
	if err := w.CanDebit(amount); err != nil {
		return err
	}
	w.Balance -= amount
	return nil
}

// Credit increases the wallet balance by amount.
func (w *Wallet) Credit(amount int64) error {
	if amount <= 0 {
		return ErrInvalidAmount
	}
	w.Balance += amount
	return nil
}
