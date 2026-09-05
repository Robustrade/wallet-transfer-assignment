package domain

import "time"

type Wallet struct {
	ID        string
	Currency  string
	Balance   Amount
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (w Wallet) CanDebit(amount Amount) bool { return w.Balance >= amount }

func (w Wallet) Debit(amount Amount) (Amount, error) {
	if err := ValidateAmount(amount); err != nil {
		return 0, err
	}
	if !w.CanDebit(amount) {
		return 0, ErrInsufficientFunds
	}
	return w.Balance - amount, nil
}

func (w Wallet) Credit(amount Amount) (Amount, error) {
	if err := ValidateAmount(amount); err != nil {
		return 0, err
	}
	if !CanAdd(w.Balance, amount) {
		return 0, newValidationError("amount", "would overflow destination balance")
	}
	return w.Balance + amount, nil
}
