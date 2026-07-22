// Package repository defines the persistence ports consumed by the service and
// worker. Implementations live in subpackages (e.g. postgres).
package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"wallet-transfer/internal/domain"
)

// Repository errors (separate from the domain business errors).
var (
	// ErrLedgerDuplicate means a ledger entry for this transfer already exists,
	// so the transfer was already processed. Used to make reprocessing safe.
	ErrLedgerDuplicate = errors.New("ledger entry already exists")
	// ErrConcurrentModification means a state update matched no row, for example
	// the reaper put the transfer back to PENDING while a worker was still on it.
	ErrConcurrentModification = errors.New("transfer changed concurrently")
)

// Store is the top-level persistence port. Its methods each run in their own
// (implicit) transaction; multi-step work goes through Tx.
type Store interface {
	// EnqueueTransfer inserts a PENDING transfer. If the key already exists it
	// returns the existing one. The bool is true only when a new row was created.
	EnqueueTransfer(ctx context.Context, p domain.NewTransferParams) (domain.Transfer, bool, error)

	// GetTransferByID returns a transfer or domain.ErrTransferNotFound.
	GetTransferByID(ctx context.Context, id uuid.UUID) (domain.Transfer, error)

	// ClaimPending moves up to limit PENDING transfers to PROCESSING for this
	// worker. FOR UPDATE SKIP LOCKED means two workers never grab the same one.
	ClaimPending(ctx context.Context, workerID string, limit int) ([]domain.Transfer, error)

	// ReapStuck puts transfers that have been PROCESSING too long back to PENDING
	// so they can be picked up again. Returns how many were reset.
	ReapStuck(ctx context.Context, olderThan time.Duration) (int64, error)

	// Tx runs fn inside a single transaction (READ COMMITTED). An error from fn
	// rolls it back; nil commits.
	Tx(ctx context.Context, fn func(TxStore) error) error
}

// TxStore is the set of steps that make up processing one transfer inside a
// single transaction. All the locking happens here.
type TxStore interface {
	// GetTransferForUpdate locks the transfer row so the reaper cannot reset it
	// while a worker is on it.
	GetTransferForUpdate(ctx context.Context, id uuid.UUID) (domain.Transfer, error)

	// LockWalletPair locks both wallets with FOR UPDATE, always in the same order
	// (by id), so opposite-direction transfers cannot deadlock.
	LockWalletPair(ctx context.Context, fromID, toID uuid.UUID) (from, to domain.Wallet, err error)

	// ApplyBalanceDelta adds delta (negative to debit) to a wallet's balance. If it
	// would go negative the database CHECK rejects it and we report insufficient funds.
	ApplyBalanceDelta(ctx context.Context, walletID uuid.UUID, delta decimal.Decimal) error

	// InsertLedgerEntry writes one entry. A repeated entry returns ErrLedgerDuplicate.
	InsertLedgerEntry(ctx context.Context, e domain.LedgerEntry) error

	// SetProcessed / SetFailed only apply when state is still PROCESSING; if nothing
	// matches they return ErrConcurrentModification.
	SetProcessed(ctx context.Context, transferID uuid.UUID) error
	SetFailed(ctx context.Context, transferID uuid.UUID, reason string) error
}
