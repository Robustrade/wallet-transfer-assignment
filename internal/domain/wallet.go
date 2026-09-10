package domain

import "time"

// Wallet is a stored-balance account. OpeningBalance is the amount at
// creation; Balance is the current authorized amount. The invariant
// Balance == OpeningBalance + credits - debits must hold after every
// committed transfer.
type Wallet struct {
	ID             string
	Balance        int64
	OpeningBalance int64
	Version        int64
	CreatedAt      time.Time
}

func NewWallet(id string, openingBalance int64, now time.Time) (Wallet, error) {
	if id == "" {
		return Wallet{}, ErrMissingWalletID
	}
	if openingBalance < 0 {
		return Wallet{}, ErrNegativeBalance
	}
	return Wallet{
		ID:             id,
		Balance:        openingBalance,
		OpeningBalance: openingBalance,
		Version:        0,
		CreatedAt:      now.UTC(),
	}, nil
}
