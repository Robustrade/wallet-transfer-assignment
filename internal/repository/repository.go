// Package repository defines the persistence contract the service layer
// depends on. It knows nothing about SQL or Postgres; internal/repository/postgres
// and internal/repository/memory each provide a concrete implementation, so
// the service can be tested against an in-memory fake and run in production
// against a real database without any code changes.
package repository

import (
	"context"

	"github.com/example/wallet-transfer/internal/domain"
)

// IdempotencyStatus tracks where a given idempotencyKey's request is in its
// lifecycle.
type IdempotencyStatus string

const (
	IdempotencyInProgress IdempotencyStatus = "IN_PROGRESS"
	IdempotencyCompleted  IdempotencyStatus = "COMPLETED"
	IdempotencyFailed     IdempotencyStatus = "FAILED"
)

// IdempotencyRecord is the stored row for a given idempotencyKey.
// ResponseBody is the JSON-encoded service.TransferResult from the first
// successful (or deterministically failed) attempt, replayed verbatim on
// duplicate requests.
type IdempotencyRecord struct {
	Key          string
	RequestHash  string
	Status       IdempotencyStatus
	TransferID   string
	ResponseBody []byte
}

// TxStore opens a new unit-of-work. Everything a single CreateTransfer call
// does — claiming the idempotency key, locking wallets, writing the ledger,
// updating balances — happens inside one Tx so it commits or rolls back
// together.
type TxStore interface {
	BeginTx(ctx context.Context) (Tx, error)
}

// Tx bundles all persistence operations needed to execute one transfer
// atomically. Implementations must guarantee that GetWalletForUpdate locks
// the row for the lifetime of the transaction (e.g. Postgres SELECT ... FOR
// UPDATE), which is what makes concurrent transfers safe.
type Tx interface {
	GetWalletForUpdate(ctx context.Context, walletID string) (domain.Wallet, error)
	UpdateWalletBalance(ctx context.Context, walletID string, newBalance int64) error

	InsertTransfer(ctx context.Context, t domain.Transfer) error
	UpdateTransferState(ctx context.Context, transferID string, state domain.TransferState, failureReason string) error

	InsertLedgerEntries(ctx context.Context, entries []domain.LedgerEntry) error

	// ClaimIdempotencyKey atomically inserts a new IN_PROGRESS record for key.
	// ok=false (with a nil error) means a record already exists for this key;
	// the caller should follow up with GetIdempotencyRecord.
	ClaimIdempotencyKey(ctx context.Context, key, requestHash string) (ok bool, err error)
	GetIdempotencyRecord(ctx context.Context, key string) (IdempotencyRecord, error)
	CompleteIdempotencyKey(ctx context.Context, key string, status IdempotencyStatus, transferID string, responseBody []byte) error

	Commit() error
	Rollback() error // safe to call after a successful Commit; must be a no-op then
}
