package service

import (
	"context"

	"wallet/internal/domain"
)

// Tx is the storage surface available inside a transfer transaction. All
// methods run on the same database transaction; implementers must not open
// their own.
type Tx interface {
	// ClaimIdempotency inserts the idempotency key and reports whether this
	// request now owns it. On conflict it waits for the conflicting
	// transaction to finish (bounded by lock_timeout) and returns false.
	ClaimIdempotency(ctx context.Context, key string, canonicalRequest []byte) (claimed bool, err error)
	// GetIdempotencyRecord reads the stored record for key, comparing the
	// stored request body against canonicalRequest.
	GetIdempotencyRecord(ctx context.Context, key string, canonicalRequest []byte) (domain.IdempotencyRecord, bool, error)
	// LockWallets locks the given wallets in a deterministic order and
	// returns them. Missing ids are simply absent from the result, so
	// len(result) < len(ids) means at least one wallet does not exist.
	// Callers must pass distinct ids.
	LockWallets(ctx context.Context, ids []string) ([]domain.Wallet, error)
	// CreateTransfer inserts a PENDING transfer and returns it.
	CreateTransfer(ctx context.Context, fromWalletID, toWalletID string, amount int64) (domain.Transfer, error)
	// DebitWallet subtracts amount from the wallet's balance. It reports
	// false (without error) if the wallet's balance is smaller than amount;
	// the balance is never allowed to go negative.
	DebitWallet(ctx context.Context, walletID string, amount int64) (ok bool, err error)
	// CreditWallet adds amount to the wallet's balance.
	CreditWallet(ctx context.Context, walletID string, amount int64) error
	// InsertLedgerEntries writes the DEBIT and CREDIT rows of a transfer.
	InsertLedgerEntries(ctx context.Context, transferID, fromWalletID, toWalletID string, amount int64) error
	// MarkProcessed moves a PENDING transfer to PROCESSED.
	MarkProcessed(ctx context.Context, transferID string) error
	// MarkFailed moves a PENDING transfer to FAILED with a reason.
	MarkFailed(ctx context.Context, transferID, reason string) error
	// CompleteIdempotency records the outcome that duplicates will replay.
	CompleteIdempotency(ctx context.Context, key string, transferID *string, status int, body []byte) error
}

// Store is the storage surface used outside a transaction: transaction
// management plus reads for the query endpoints.
type Store interface {
	// InTx runs fn inside one database transaction. fn's error (or a failure
	// to commit) rolls the transaction back; a nil error from fn commits it.
	InTx(ctx context.Context, fn func(ctx context.Context, tx Tx) error) error
	// GetIdempotencyRecord is Tx.GetIdempotencyRecord on its own connection,
	// used to resolve the outcome of a commit whose result is unknown.
	GetIdempotencyRecord(ctx context.Context, key string, canonicalRequest []byte) (domain.IdempotencyRecord, bool, error)
	GetWallet(ctx context.Context, id string) (domain.Wallet, error)
	GetTransfer(ctx context.Context, id string) (domain.Transfer, error)
	ListWalletLedger(ctx context.Context, walletID string, limit int) ([]domain.LedgerEntry, error)
}
