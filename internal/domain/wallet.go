package domain

import "math"

// Amount represents money in the smallest unit of a currency (for example, paise).
// It is an integer to avoid floating-point rounding errors.
type Amount int64

// Wallet is the domain representation of a wallet and its available balance.
type Wallet struct {
	ID       string
	Currency string
	Balance  Amount
}

func (w Wallet) Validate() error {
	if w.ID == "" {
		return ErrInvalidWalletID
	}
	if w.Currency == "" {
		return ErrInvalidCurrency
	}
	if w.Balance < 0 {
		return ErrInvalidBalance
	}
	return nil
}

// Debit decreases the wallet balance without allowing a negative balance.
func (w *Wallet) Debit(amount Amount) error {
	if amount <= 0 {
		return ErrInvalidAmount
	}
	if w.Balance < amount {
		return ErrInsufficientFunds
	}
	w.Balance -= amount
	return nil
}

// Credit increases the wallet balance without overflowing Amount.
func (w *Wallet) Credit(amount Amount) error {
	if amount <= 0 {
		return ErrInvalidAmount
	}
	if amount > Amount(math.MaxInt64)-w.Balance {
		return ErrBalanceOverflow
	}
	w.Balance += amount
	return nil
}
