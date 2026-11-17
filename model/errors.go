package model

import "errors"

// Domain and persistence sentinel errors. Single source of truth for the domain.
// Service-layer API outcomes (wallet_not_found, idempotency mismatch, …) live in service/errors.go.
var (
	ErrInvalidAmount           = errors.New("amount must be positive")
	ErrNegativeBalance         = errors.New("balance must be non-negative")
	ErrBalanceOverflow         = errors.New("amount would overflow wallet balance")
	ErrInvalidWalletID         = errors.New("wallet id must be non-empty")
	ErrInvalidIdempotencyKey   = errors.New("idempotency key must be non-empty")
	ErrSameWallet              = errors.New("from and to wallets must differ")
	ErrOpeningBalanceImmutable = errors.New("opening_balance is immutable")
	ErrInvalidTransition       = errors.New("invalid transfer status transition")
	ErrInvalidFailureReason    = errors.New("failure reason required when failed")
	ErrInvalidEntryType        = errors.New("entry type must be DEBIT or CREDIT")
	ErrLedgerRequiresTransfer  = errors.New("ledger entry requires a processed transfer")
	ErrNotFound                = errors.New("not found")
	ErrConflict                = errors.New("conflict")
)
