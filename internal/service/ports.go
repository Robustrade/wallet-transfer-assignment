package service

import (
	"context"
	"database/sql"

	"wallettransfer/internal/domain"
)

// TxBeginner abstracts *sql.DB so the service can start a transaction
// without depending on a concrete driver. Both *sql.DB and test doubles
// satisfy this.
type TxBeginner interface {
	BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error)
}

// WalletRepo defines wallet persistence operations. LockForUpdate takes an
// explicit *sql.Tx because row locks are only meaningful within a single
// transaction — this keeps that requirement visible in the type signature
// rather than hidden inside the implementation.
type WalletRepo interface {
	LockForUpdate(ctx context.Context, tx *sql.Tx, walletID string) (domain.Wallet, error)
	UpdateBalance(ctx context.Context, tx *sql.Tx, walletID string, newBalance int64) error
	Get(ctx context.Context, walletID string) (domain.Wallet, error)
	Create(ctx context.Context, walletID string, openingBalance int64) (domain.Wallet, error)
}

// TransferRepo persists transfer records and their state transitions.
type TransferRepo interface {
	Insert(ctx context.Context, tx *sql.Tx, t domain.Transfer) error
	UpdateStatus(ctx context.Context, tx *sql.Tx, transferID string, status domain.TransferStatus, failureReason string) error
	Get(ctx context.Context, transferID string) (domain.Transfer, error)
}

// LedgerRepo persists the double-entry ledger legs.
type LedgerRepo interface {
	InsertEntries(ctx context.Context, tx *sql.Tx, entries []domain.LedgerEntry) error
}

// IdempotencyRepo implements the exactly-once claim/replay protocol.
//
// Claim attempts to atomically "own" an idempotency key for a given request
// fingerprint (a hash of the request body). Three outcomes are possible:
//
//  1. claimed=true: no prior record existed; the caller now owns this key
//     within the current transaction and must call Complete before commit.
//  2. claimed=false, rec.Status == "COMPLETED": a prior request with the
//     same key and fingerprint already finished — the caller should replay
//     rec's stored response verbatim without redoing any business logic.
//  3. claimed=false with a fingerprint mismatch: the same key was reused
//     for a logically different request — this is a client error
//     (domain.ErrIdempotencyConflict), not a retry.
//
// The concurrent-duplicate case (two requests with the same key racing each
// other) is handled by PostgreSQL's native INSERT ... ON CONFLICT blocking
// behavior: a second concurrent claim physically blocks at the database
// until the first transaction commits or rolls back, and only then
// evaluates whether a conflict exists. See the postgres implementation for
// details.
type IdempotencyRepo interface {
	Claim(ctx context.Context, tx *sql.Tx, key, requestFingerprint string) (rec domain.IdempotencyRecord, claimed bool, err error)
	Complete(ctx context.Context, tx *sql.Tx, key string, transferID string, responseStatus int, responseBody []byte) error
}
