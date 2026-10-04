package domain

import "time"

// Wallet is an account that can send and receive transfers.
// Balance is the current stored balance. InitialBalance is the opening
// amount recorded at creation and is not itself a ledger entry.
type Wallet struct {
	ID             string
	InitialBalance int64
	Balance        int64
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// NewWallet validates an opening wallet. Balance starts equal to initialBalance.
func NewWallet(id string, initialBalance int64, now time.Time) (Wallet, error) {
	if err := ValidateWalletID(id); err != nil {
		return Wallet{}, err
	}
	if err := ValidateInitialBalance(initialBalance); err != nil {
		return Wallet{}, err
	}
	now = now.UTC()
	return Wallet{
		ID:             id,
		InitialBalance: initialBalance,
		Balance:        initialBalance,
		CreatedAt:      now,
		UpdatedAt:      now,
	}, nil
}
