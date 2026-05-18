package domain

import "time"

type Wallet struct {
	ID        string
	Balance   int64
	CreatedAt time.Time
	UpdatedAt time.Time
}

func NewWallet(id string, balance int64) (*Wallet, error) {
	if id == "" {
		return nil, ErrInvalidWalletID
	}

	if balance < 0 {
		return nil, ErrNegativeBalance
	}

	return &Wallet{
		ID:      id,
		Balance: balance,
	}, nil
}

func (w *Wallet) CanDebit(amount int64) bool {
	return w.Balance >= amount
}

func (w *Wallet) Debit(amount int64) error {
	if amount <= 0 {
		return ErrInvalidAmount
	}

	if !w.CanDebit(amount) {
		return ErrInsufficientFunds
	}

	w.Balance -= amount
	return nil
}

func (w *Wallet) Credit(amount int64) error {
	if amount <= 0 {
		return ErrInvalidAmount
	}

	w.Balance += amount
	return nil
}
