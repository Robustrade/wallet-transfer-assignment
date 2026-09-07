package domain

import "time"

// Wallet holds a stored balance.
type Wallet struct {
	ID        string
	Balance   int64
	CreatedAt time.Time
	UpdatedAt time.Time
}

func NewWallet(id string, initialBalance int64) (*Wallet, error) {
	if id == "" {
		return nil, ErrInvalidWalletID
	}
	if initialBalance < 0 {
		return nil, ErrNegativeBalance
	}
	now := time.Now().UTC()
	return &Wallet{
		ID:        id,
		Balance:   initialBalance,
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

// CanDebit reports whether amount can be subtracted.
func (w *Wallet) CanDebit(amount int64) bool {
	return amount > 0 && w.Balance >= amount
}
