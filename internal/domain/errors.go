package domain

import "errors"

var (
	// ErrInsufficientFunds is returned when the source wallet does not have enough balance.
	ErrInsufficientFunds = errors.New("insufficient funds in source wallet")

	// ErrWalletNotFound is returned when a requested wallet does not exist.
	ErrWalletNotFound = errors.New("wallet not found")

	// ErrSameWalletTransfer is returned when source and destination wallets are identical.
	ErrSameWalletTransfer = errors.New("source and destination wallets cannot be the same")

	// ErrInvalidAmount is returned when the transfer amount is less than or equal to zero.
	ErrInvalidAmount = errors.New("transfer amount must be strictly positive")

	// ErrIdempotencyConflict is returned when an idempotency key is reused with differing parameters.
	ErrIdempotencyConflict = errors.New("idempotency key reused with different request payload")

	// ErrIdempotencyInProgress is returned when another request with the same idempotency key is currently executing.
	ErrIdempotencyInProgress = errors.New("request with this idempotency key is currently in progress")

	// ErrInvalidStateTransition is returned when an illegal transfer state transition is attempted.
	ErrInvalidStateTransition = errors.New("invalid transfer state transition")

	// ErrMissingIdempotencyKey is returned when the idempotency key is empty.
	ErrMissingIdempotencyKey = errors.New("idempotency key is required")

	// ErrMissingWalletID is returned when the source or destination wallet ID is empty.
	ErrMissingWalletID = errors.New("source and destination wallet IDs are required")

	// ErrWalletNameRequired is returned when the wallet name is empty.
	ErrWalletNameRequired = errors.New("wallet name is required")

	// ErrNegativeBalance is returned when a negative initial balance is provided.
	ErrNegativeBalance = errors.New("initial balance cannot be negative")

	// ErrBalanceOverflow is returned when crediting an amount would overflow the maximum wallet balance.
	ErrBalanceOverflow = errors.New("balance overflow: maximum wallet balance exceeded")

	// ErrCurrencyMismatch is returned when source and destination wallets have different currencies.
	ErrCurrencyMismatch = errors.New("source and destination wallets have different currencies")

	// ErrTransferNotFound is returned when a requested transfer does not exist.
	ErrTransferNotFound = errors.New("transfer not found")

	// ErrWalletAlreadyExists is returned when attempting to create a wallet with an existing ID.
	ErrWalletAlreadyExists = errors.New("wallet already exists")

	// ErrInvalidIdempotencyKey is returned when the idempotency key exceeds maximum length.
	ErrInvalidIdempotencyKey = errors.New("idempotency key must not exceed 128 characters")

	// ErrInvalidLedgerPair is returned when ledger entries do not form a balanced double-entry pair.
	ErrInvalidLedgerPair = errors.New("ledger entries must contain exactly one matching DEBIT and CREDIT pair")

	// ErrIdempotencyLeaseLost is returned when a worker attempts to finalize or cleanup an idempotency reservation whose lease was lost.
	ErrIdempotencyLeaseLost = errors.New("idempotency reservation lease was lost to a concurrent worker")

	// ErrUnauthorizedFunding is returned when an unauthenticated caller attempts to create a wallet with a positive initial balance.
	ErrUnauthorizedFunding = errors.New("initial balance funding requires administrative authorization")

	// ErrSystemTreasuryRestricted is returned when a caller attempts to use system_treasury as a source or destination in transfers.
	ErrSystemTreasuryRestricted = errors.New("system treasury cannot be used in public transfers")

	// ErrInvalidWalletID is returned when the wallet ID exceeds maximum length (64 characters).
	ErrInvalidWalletID = errors.New("wallet ID must not exceed 64 characters")

	// ErrInvalidWalletName is returned when the wallet name exceeds maximum length (255 characters).
	ErrInvalidWalletName = errors.New("wallet name must not exceed 255 characters")

	// ErrInvalidCurrency is returned when the currency code is not a valid 3-letter code.
	ErrInvalidCurrency = errors.New("currency must be a valid 3-letter code")
)
