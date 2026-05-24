package domain

import "errors"

var (
	ErrWalletNotFound       = errors.New("wallet not found")
	ErrInsufficientFunds    = errors.New("insufficient funds")
	ErrInvalidAmount        = errors.New("invalid amount")
	ErrSameWalletTransfer   = errors.New("cannot transfer to same wallet")
	ErrTransferNotFound     = errors.New("transfer not found")
	ErrInvalidTransferState = errors.New("invalid transfer state")
)
