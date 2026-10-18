package domain

import "errors"

var (
	ErrInvalidAmount     = errors.New("amount must be greater than zero")
	ErrInsufficientFunds = errors.New("insufficient wallet balance")
	ErrWalletNotFound    = errors.New("wallet not found")
)

type Wallet struct {
	ID      int64
	Balance int64
}

func (w Wallet) CanDebit(amount int64) error {
	if amount <= 0 {
		return ErrInvalidAmount
	}
	if w.Balance < amount {
		return ErrInsufficientFunds
	}
	return nil
}
