// Package service holds business logic and orchestration: idempotency,
// two-phase execution, lock ordering, and transfer state transitions. It
// depends on persistence only through the interfaces declared here, so it can
// be unit-tested with in-memory fakes.
package service

import (
	"context"
	"errors"

	"github.com/shopspring/decimal"

	"github.com/mayankkandpal18/wallet-transfer-assignment/internal/domain"
	"github.com/mayankkandpal18/wallet-transfer-assignment/internal/repository"
)

// WalletStore is the wallet persistence the service needs. GetForUpdate is
// used in both phases (sorted lock order); existence is enforced by it too.
type WalletStore interface {
	GetForUpdate(ctx context.Context, db repository.DBTX, id string) (domain.Wallet, error)
	AddToBalance(ctx context.Context, db repository.DBTX, id string, delta decimal.Decimal) error
}

// TransferStore is the transfer persistence the service needs.
type TransferStore interface {
	InsertPending(ctx context.Context, db repository.DBTX, t domain.Transfer) (domain.Transfer, error)
	GetByIdempotencyKey(ctx context.Context, db repository.DBTX, key string) (domain.Transfer, error)
	GetByID(ctx context.Context, db repository.DBTX, id string) (domain.Transfer, error)
	UpdateStatus(ctx context.Context, db repository.DBTX, id string, status domain.TransferStatus, failureReason string) error
}

// LedgerStore is the ledger persistence the service needs.
type LedgerStore interface {
	InsertEntry(ctx context.Context, db repository.DBTX, e domain.LedgerEntry) error
}

// Transactor provides the transaction boundary. WithTx runs fn in one
// committed transaction; DB is a non-transactional handle for the
// fast-path/pre-check reads that must not open a transaction.
type Transactor interface {
	WithTx(ctx context.Context, fn func(repository.DBTX) error) error
	DB() repository.DBTX
}

// CreateTransferInput is the validated-on-arrival request shape.
type CreateTransferInput struct {
	IdempotencyKey string
	FromWalletID   string
	ToWalletID     string
	Amount         decimal.Decimal
}

// TransferService orchestrates idempotent, concurrency-safe transfers.
type TransferService struct {
	tx        Transactor
	wallets   WalletStore
	transfers TransferStore
	ledger    LedgerStore
}

func NewTransferService(tx Transactor, w WalletStore, t TransferStore, l LedgerStore) *TransferService {
	return &TransferService{tx: tx, wallets: w, transfers: t, ledger: l}
}

// GetTransfer returns a transfer by id (used by the read endpoint).
func (s *TransferService) GetTransfer(ctx context.Context, id string) (domain.Transfer, error) {
	return s.transfers.GetByID(ctx, s.tx.DB(), id)
}

// CreateTransfer executes a wallet-to-wallet transfer with exactly-once
// semantics per idempotency key.
//
// The bool return is `created`: true only when this call inserted a new
// PENDING row (HTTP 201); false when an existing transfer for the key was
// returned instead (duplicate, HTTP 200).
//
// A returned (Transfer, _, nil) is the outcome to surface to the client even
// when the business result is FAILED (insufficient funds): the request was
// valid and the attempt is audited. A non-nil error means the request itself
// was invalid (validation, wallet-not-found) or a persistence failure occurred.
func (s *TransferService) CreateTransfer(ctx context.Context, in CreateTransferInput) (domain.Transfer, bool, error) {
	if err := domain.ValidateTransferInput(in.IdempotencyKey, in.FromWalletID, in.ToWalletID, in.Amount); err != nil {
		return domain.Transfer{}, false, err
	}

	// Layer 1 — fast path: an already-recorded transfer for this key is
	// returned in its current state (PENDING/PROCESSED/FAILED) with no side
	// effects and without re-running Phase 2.
	existing, err := s.transfers.GetByIdempotencyKey(ctx, s.tx.DB(), in.IdempotencyKey)
	switch {
	case err == nil:
		return existing, false, nil
	case errors.Is(err, domain.ErrTransferNotFound):
		// not seen before; continue
	default:
		return domain.Transfer{}, false, err
	}

	// Lock ordering: sort the two wallet ids so every transaction (Phase 1 and
	// Phase 2) acquires wallet locks in the same global order.
	lo, hi := in.FromWalletID, in.ToWalletID
	if lo > hi {
		lo, hi = hi, lo
	}

	// Phase 1 — claim the idempotency key by committing a PENDING row.
	//
	// The two wallet rows are locked FOR UPDATE in sorted order *before* the
	// INSERT. The INSERT's FK checks would otherwise take implicit row-share
	// locks on both wallets in unsorted (column) order, which can deadlock
	// (SQLSTATE 40P01) with an opposing transfer's Phase 1/Phase 2. Pre-locking
	// in sorted order keeps the global lock order consistent. These locked
	// reads also enforce wallet existence: a missing wallet returns 404 and the
	// transaction rolls back, so no PENDING row is written.
	var pending domain.Transfer
	err = s.tx.WithTx(ctx, func(db repository.DBTX) error {
		if _, e := s.wallets.GetForUpdate(ctx, db, lo); e != nil {
			return e
		}
		if _, e := s.wallets.GetForUpdate(ctx, db, hi); e != nil {
			return e
		}
		p, e := s.transfers.InsertPending(ctx, db, domain.Transfer{
			IdempotencyKey: in.IdempotencyKey,
			FromWalletID:   in.FromWalletID,
			ToWalletID:     in.ToWalletID,
			Amount:         in.Amount,
		})
		if e != nil {
			return e
		}
		pending = p
		return nil
	})
	if errors.Is(err, domain.ErrDuplicateIdempotencyKey) {
		// Layer 2/3 — a concurrent request won the unique constraint between
		// our Layer 1 read and this insert. Return its current state.
		dup, e := s.transfers.GetByIdempotencyKey(ctx, s.tx.DB(), in.IdempotencyKey)
		if e != nil {
			return domain.Transfer{}, false, e
		}
		return dup, false, nil
	}
	if err != nil {
		return domain.Transfer{}, false, err
	}

	// Phase 2 — business logic in its own committed transaction. Re-lock the
	// two wallets in the same sorted order (two separate FOR UPDATE statements)
	// and re-read balances under the lock.
	err = s.tx.WithTx(ctx, func(db repository.DBTX) error {
		wLo, e := s.wallets.GetForUpdate(ctx, db, lo)
		if e != nil {
			return e
		}
		wHi, e := s.wallets.GetForUpdate(ctx, db, hi)
		if e != nil {
			return e
		}
		fromWallet := wLo
		if in.FromWalletID == hi {
			fromWallet = wHi
		}

		if !fromWallet.HasSufficientFunds(in.Amount) {
			// Commit a FAILED row (no ledger, no balance change). This is a
			// terminal business outcome, not an error.
			return s.transfers.UpdateStatus(ctx, db, pending.ID,
				domain.TransferFailed, domain.ErrInsufficientFunds.Error())
		}

		if e := s.ledger.InsertEntry(ctx, db, domain.LedgerEntry{
			TransferID: pending.ID, WalletID: in.FromWalletID,
			Type: domain.LedgerDebit, Amount: in.Amount,
		}); e != nil {
			return e
		}
		if e := s.ledger.InsertEntry(ctx, db, domain.LedgerEntry{
			TransferID: pending.ID, WalletID: in.ToWalletID,
			Type: domain.LedgerCredit, Amount: in.Amount,
		}); e != nil {
			return e
		}
		if e := s.wallets.AddToBalance(ctx, db, in.FromWalletID, in.Amount.Neg()); e != nil {
			return e
		}
		if e := s.wallets.AddToBalance(ctx, db, in.ToWalletID, in.Amount); e != nil {
			return e
		}
		return s.transfers.UpdateStatus(ctx, db, pending.ID, domain.TransferProcessed, "")
	})
	if err != nil {
		// Phase 2 failed: the PENDING row remains for reconciliation; no
		// partial side effects (the transaction rolled back).
		return domain.Transfer{}, false, err
	}

	final, err := s.transfers.GetByID(ctx, s.tx.DB(), pending.ID)
	if err != nil {
		return domain.Transfer{}, false, err
	}
	return final, true, nil
}
