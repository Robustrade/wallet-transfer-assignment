package domain

import "errors"

var (
	ErrWalletNotFound   = errors.New("wallet not found")
	ErrInsufficientFunds = errors.New("insufficient funds")
	ErrTransferNotFound = errors.New("transfer not found")
	ErrSameWallet       = errors.New("source and destination wallets must be different")
	ErrInvalidAmount    = errors.New("amount must be greater than zero")
)
