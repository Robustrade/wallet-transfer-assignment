// Package repository defines persistence interfaces consumed by the service
// layer. Concrete implementations live in subpackages (e.g. postgres).
//
// The interfaces here are the boundary between business logic (service)
// and storage. They're deliberately small: no batch operations, no
// query builders, no streaming. Every method takes an Executor so the
// same code path works inside a transaction (*sql.Tx) and outside
// (*sql.DB) — the service is the only layer that decides which.
package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/PratikDhanave/wallet-transfer-assignment/internal/domain"
)

// Sentinel errors returned by every implementation. Service code uses
// errors.Is against these; never string-match the message text.
var (
	// ErrNotFound is returned by Get* methods when the row does not
	// exist. The handler maps this (via classify) to HTTP 404.
	ErrNotFound = errors.New("not found")

	// ErrIdempotencyExists is returned by IdempotencyRepository.Insert
	// when the key already has a row. This is the signal the service
	// uses to switch from "create new transfer" to "replay existing"
	// — see service/transfer.go.
	ErrIdempotencyExists = errors.New("idempotency key already exists")
)

// Executor is the read/write contract shared by *sql.DB and *sql.Tx.
// Passing it to every method lets the service decide whether a given
// call participates in a surrounding transaction without the repository
// having to know.
type Executor interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// WalletRepository persists wallets. Balance arithmetic itself lives in
// the service layer; this interface only stores and retrieves rows.
type WalletRepository interface {
	// Get returns the wallet for id, or ErrNotFound. Use only when no
	// mutation will follow (or the surrounding tx already holds a
	// FOR UPDATE lock).
	Get(ctx context.Context, exec Executor, id string) (*domain.Wallet, error)

	// LockForUpdate acquires a row-level lock for the lifetime of the
	// surrounding transaction. Callers MUST lock wallets in a
	// deterministic order (lex ascending by id) to avoid deadlocks.
	LockForUpdate(ctx context.Context, exec Executor, id string) (*domain.Wallet, error)

	// UpdateBalance writes a new absolute balance. Service code
	// computes the new balance after locking + reading the old one;
	// this method does not do `balance = balance - X` math.
	UpdateBalance(ctx context.Context, exec Executor, id string, balance int64) error

	// Insert creates a new wallet row. Mainly used by tests and the
	// POST /wallets convenience endpoint.
	Insert(ctx context.Context, exec Executor, w *domain.Wallet) error
}

// TransferRepository persists transfer rows. The state machine is
// owned by the service; this interface just persists what the service
// computes.
type TransferRepository interface {
	Get(ctx context.Context, exec Executor, id uuid.UUID) (*domain.Transfer, error)

	// Insert writes a new transfer row, typically in StatePending
	// inside the same tx that will then drive it to a terminal state.
	Insert(ctx context.Context, exec Executor, t *domain.Transfer) error

	// UpdateState moves the transfer to a new state with an optional
	// failure reason. The schema CHECK constraint on `state` plus the
	// service-level CanTransitionTo check enforce valid moves.
	UpdateState(ctx context.Context, exec Executor, id uuid.UUID, state domain.TransferState, failureReason string) error

	// ListByWallet returns transfers where the wallet appears as either
	// source or destination, ordered by created_at DESC then id DESC
	// (the tiebreaker matches the composite index ordering).
	//
	// Pagination is cursor-based: pass `before = nil` for the first
	// page; for subsequent pages pass the createdAt of the last item
	// from the previous page. The limit is the caller's responsibility
	// to bound — the service layer enforces a sensible default + max.
	ListByWallet(ctx context.Context, exec Executor, walletID string, limit int, before *time.Time) ([]domain.Transfer, error)
}

// LedgerRepository writes the append-only ledger entries.
type LedgerRepository interface {
	// Insert writes one ledger entry. Two entries (DEBIT + CREDIT)
	// per transfer are written by the service inside the same tx;
	// the schema's UNIQUE (transfer_id, type) index makes a third
	// insert impossible.
	Insert(ctx context.Context, exec Executor, e *domain.LedgerEntry) error

	// ListByTransfer returns the entries for a transfer ordered by
	// id (i.e. insertion order). Used by tests and any
	// transfer-history endpoint.
	ListByTransfer(ctx context.Context, exec Executor, transferID uuid.UUID) ([]domain.LedgerEntry, error)
}

// IdempotencyRecord is the durable mapping from an idempotency key to
// the side-effect (transfer) it produced, plus a hash of the request
// body so replays with the same key but different payload can be
// rejected.
type IdempotencyRecord struct {
	Key         string
	RequestHash string
	TransferID  uuid.UUID
}

// IdempotencyRepository persists idempotency records.
//
// The PRIMARY KEY on `key` is the actual serialisation primitive:
// concurrent first-time inserts block on the unique-index lock; the
// loser receives ErrIdempotencyExists (mapped from SQLSTATE 23505) and
// the service replays the winner's outcome.
type IdempotencyRepository interface {
	Get(ctx context.Context, exec Executor, key string) (*IdempotencyRecord, error)

	// Insert stores a new idempotency record. If the key already
	// exists it returns ErrIdempotencyExists — callers use that
	// signal to switch to the replay path rather than retrying.
	Insert(ctx context.Context, exec Executor, key, hash string, transferID uuid.UUID) error
}

// TxManager runs a function inside a database transaction. The
// transaction commits if fn returns nil and rolls back on a non-nil
// error or a panic. See postgres.TxManager for the implementation
// details.
type TxManager interface {
	RunInTx(ctx context.Context, fn func(Executor) error) error
}
