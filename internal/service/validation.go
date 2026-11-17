
package service

import (
	"errors"
	"strings"
)

var (
	ErrInvalidAmount = errors.New("amount must be greater than zero")
	ErrInvalidWallet = errors.New("wallet IDs are required")
	ErrSameWallet    = errors.New("source and destination wallets must be different")
)

func ValidateTransfer(fromWalletID, toWalletID string, amount int64) error {
	if amount <= 0 {
		return ErrInvalidAmount
	}

	if strings.TrimSpace(fromWalletID) == "" ||
		strings.TrimSpace(toWalletID) == "" {
		return ErrInvalidWallet
	}

	if fromWalletID == toWalletID {
		return ErrSameWallet
	}

	return nil
}
