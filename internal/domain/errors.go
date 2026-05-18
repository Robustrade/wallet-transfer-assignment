package domain

import "errors"

var (
	ErrInvalidWalletID        = errors.New("invalid wallet id")
	ErrInvalidTransferID      = errors.New("invalid transfer id")
	ErrMissingIdempotencyKey  = errors.New("missing idempotency key")
	ErrInvalidAmount          = errors.New("amount must be greater than zero")
	ErrNegativeBalance        = errors.New("wallet balance cannot be negative")
	ErrInsufficientFunds      = errors.New("insufficient funds")
	ErrSameWalletTransfer     = errors.New("source and destination wallet cannot be same")
	ErrInvalidTransferState   = errors.New("invalid transfer state transition")
	ErrInvalidLedgerEntryType = errors.New("invalid ledger entry type")
	ErrIdempotencyConflict    = errors.New("idempotency key reused with different request payload")
	ErrIdempotencyInProgress  = errors.New("idempotency request is still in progress")
)
