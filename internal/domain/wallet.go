package domain

import (
	"time"

	"github.com/google/uuid"
)

type Wallet struct {
	ID        uuid.UUID `json:"walletId"`
	Balance   Money     `json:"balance"`
	Currency  string    `json:"currency"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func NewWallet(id uuid.UUID, initialBalance Money, currency string) (*Wallet, error) {
	if initialBalance.IsNegative() {
		return nil, ErrInvalidAmount("wallet balance cannot be negative")
	}
	if currency == "" {
		currency = "USD"
	}
	now := time.Now().UTC()
	return &Wallet{
		ID:        id,
		Balance:   initialBalance,
		Currency:  currency,
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

func (w *Wallet) Debit(amount Money) error {
	if !amount.IsPositive() {
		return ErrInvalidAmount("debit amount must be greater than zero")
	}
	if !w.Balance.GreaterThanOrEqual(amount) {
		return ErrInsufficientFunds()
	}
	w.Balance = w.Balance.Sub(amount)
	w.UpdatedAt = time.Now().UTC()
	return nil
}

func (w *Wallet) Credit(amount Money) error {
	if !amount.IsPositive() {
		return ErrInvalidAmount("credit amount must be greater than zero")
	}
	w.Balance = w.Balance.Add(amount)
	w.UpdatedAt = time.Now().UTC()
	return nil
}
