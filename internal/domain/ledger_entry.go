package domain

import "time"

type LedgerEntryType string

const (
	LedgerEntryDebit  LedgerEntryType = "DEBIT"
	LedgerEntryCredit LedgerEntryType = "CREDIT"
)

type LedgerEntry struct {
	ID           int64
	TransferID   string
	WalletID     string
	Type         LedgerEntryType
	Amount       int64
	BalanceAfter *int64
	CreatedAt    time.Time
}

func NewDebit(transferID, walletID string, money Money, balanceAfter int64) (LedgerEntry, error) {
	if money.Amount <= 0 {
		return LedgerEntry{}, ErrInvalidAmount
	}
	return LedgerEntry{
		TransferID:   transferID,
		WalletID:     walletID,
		Type:         LedgerEntryDebit,
		Amount:       money.Amount,
		BalanceAfter: &balanceAfter,
	}, nil
}

func NewCredit(transferID, walletID string, money Money, balanceAfter int64) (LedgerEntry, error) {
	if money.Amount <= 0 {
		return LedgerEntry{}, ErrInvalidAmount
	}
	return LedgerEntry{
		TransferID:   transferID,
		WalletID:     walletID,
		Type:         LedgerEntryCredit,
		Amount:       money.Amount,
		BalanceAfter: &balanceAfter,
	}, nil
}
