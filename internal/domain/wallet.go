package domain

import "time"

type WalletStatus string

const (
	WalletStatusActive WalletStatus = "ACTIVE"
	WalletStatusFrozen WalletStatus = "FROZEN"
	WalletStatusClosed WalletStatus = "CLOSED"
)

type Wallet struct {
	ID             string
	OwnerID        string
	Currency       string
	OpeningBalance int64
	Balance        int64
	Status         WalletStatus
	Version        int64
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func (wallet Wallet) CanDebit(money Money) error {
	if wallet.Status != WalletStatusActive {
		return ErrWalletNotActive
	}
	if wallet.Currency != money.Currency {
		return ErrCurrencyMismatch
	}
	if wallet.Balance < money.Amount {
		return ErrInsufficientFunds
	}
	return nil
}

func (wallet Wallet) CanCredit(currency string) error {
	if wallet.Status != WalletStatusActive {
		return ErrWalletNotActive
	}
	if wallet.Currency != NormalizeCurrency(currency) {
		return ErrCurrencyMismatch
	}
	return nil
}
