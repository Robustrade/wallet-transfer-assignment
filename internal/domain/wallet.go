package domain

import "time"

// Wallet is an account holding a balance denominated in the smallest currency
// unit. Balance mutations go through Debit and Credit so the invariants
// (positive amounts, no overdraft) live in one place and cannot be bypassed.
type Wallet struct {
	ID        string
	Balance   Money
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Debit removes amount from the wallet, enforcing a positive amount and
// rejecting overdrafts. The caller is responsible for holding the appropriate
// row lock before invoking Debit under concurrency.
func (w *Wallet) Debit(amount Money) error {
	if err := ValidateAmount(amount); err != nil {
		return err
	}
	if w.Balance < amount {
		return ErrInsufficientFunds
	}
	w.Balance -= amount
	return nil
}

// Credit adds amount to the wallet, enforcing a positive amount.
func (w *Wallet) Credit(amount Money) error {
	if err := ValidateAmount(amount); err != nil {
		return err
	}
	w.Balance += amount
	return nil
}
