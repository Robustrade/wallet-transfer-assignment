package domain

import "fmt"

func NewWallet(id string, initialBalance int64) (*Wallet, error) {
	switch {
	case id == "":
		return nil, newValidationError("walletId", "is required")
	case len(id) > maxIdentifierLength:
		return nil, newValidationError("walletId", fmt.Sprintf("must be at most %d characters", maxIdentifierLength))
	case initialBalance < 0:
		return nil, newValidationError("initialBalance", "must not be negative")
	}
	return &Wallet{ID: id, Balance: initialBalance}, nil
}

func (w *Wallet) Debit(amount int64) error {
	if amount <= 0 {
		return newValidationError("amount", "must be greater than zero")
	}
	if w.Balance < amount {
		return ErrInsufficientFunds
	}
	w.Balance -= amount
	return nil
}

func (w *Wallet) Credit(amount int64) error {
	if amount <= 0 {
		return newValidationError("amount", "must be greater than zero")
	}
	w.Balance += amount
	return nil
}
