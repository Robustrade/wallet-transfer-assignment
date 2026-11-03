package domain

import "errors"

var (
	ErrInvalidTransferID         = errors.New("transfer ID is required")
	ErrInvalidWalletID           = errors.New("wallet ID is required")
	ErrSameWallet                = errors.New("source and destination wallets must differ")
	ErrInvalidAmount             = errors.New("amount must be greater than zero")
	ErrInvalidTransferStatus     = errors.New("invalid transfer status")
	ErrInvalidTransferTransition = errors.New("invalid transfer state transition")
	ErrInvalidFailureCode        = errors.New("failure code is required")
	ErrInvalidCurrency           = errors.New("currency is required")
	ErrInvalidBalance            = errors.New("wallet balance cannot be negative")
	ErrInsufficientFunds         = errors.New("insufficient funds")
	ErrBalanceOverflow           = errors.New("wallet balance overflow")
	ErrInvalidLedgerEntry        = errors.New("invalid ledger entry")
	ErrUnbalancedLedger          = errors.New("ledger entries do not form a balanced transfer pair")
)
