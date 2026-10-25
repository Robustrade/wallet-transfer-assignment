// Package service holds the business workflows. It owns transaction
// boundaries and orchestration; persistence details stay in the repositories
// and invariants stay in the domain.
package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/GaureshPatil/wallet-transfer-assignment/internal/domain"
	"github.com/GaureshPatil/wallet-transfer-assignment/internal/repository"
)

// CreateTransferInput is a request to move Amount minor units between wallets.
type CreateTransferInput struct {
	IdempotencyKey string
	FromWalletID   string
	ToWalletID     string
	Amount         int64
}

// TransferResult is the outcome of CreateTransfer. Transfer is always terminal
// (PROCESSED or FAILED). A business failure such as insufficient funds is a
// result, not an error.
type TransferResult struct {
	Transfer *domain.Transfer
	// Replayed is true when the request repeated an earlier idempotency key and
	// Transfer is the original outcome rather than a new execution.
	Replayed bool
}

// TransferDetails is a transfer together with its ledger entries.
type TransferDetails struct {
	Transfer *domain.Transfer
	Entries  []domain.LedgerEntry
}

// TransferService executes wallet-to-wallet transfers.
type TransferService struct {
	store repository.Store
	now   func() time.Time
	newID func() uuid.UUID
}

// utcMicros is the service clock. PostgreSQL stores timestamps with microsecond
// precision, so values are truncated up front: the response to the first request
// then equals a later replay that is read back from the database.
func utcMicros() time.Time { return time.Now().UTC().Truncate(time.Microsecond) }

// NewTransferService builds a TransferService on top of store.
func NewTransferService(store repository.Store) *TransferService {
	return &TransferService{
		store: store,
		now:   utcMicros,
		newID: uuid.New,
	}
}

// CreateTransfer validates the request and executes it in a single database
// transaction: claim the idempotency key, lock both wallets, move the money,
// write the two ledger entries and mark the transfer PROCESSED, or mark it
// FAILED when funds are short. Everything commits together or not at all, so a
// crash can never leave a key claimed without its transfer, or a transfer
// without its ledger entries.
//
// Repeating a key returns the original outcome (Replayed = true) without any
// new side effect; reusing a key with a different payload is rejected with
// domain.ErrIdempotencyKeyReused.
func (s *TransferService) CreateTransfer(ctx context.Context, in CreateTransferInput) (*TransferResult, error) {
	if err := domain.ValidateTransferRequest(in.IdempotencyKey, in.FromWalletID, in.ToWalletID, in.Amount); err != nil {
		return nil, err
	}

	hash := requestHash(in)

	var result *TransferResult
	err := s.store.WithinTx(ctx, func(r repository.Repositories) error {
		// WithinTx may re-run this closure after a transient failure; always
		// start from a clean slate.
		result = nil

		// Claim the key first. The unique index makes concurrent duplicates
		// wait here until the first request commits or rolls back.
		claimed, err := r.Idempotency().Claim(ctx, in.IdempotencyKey, hash, s.now())
		if err != nil {
			return err
		}
		if !claimed {
			result, err = s.replay(ctx, r, in.IdempotencyKey, hash)
			return err
		}

		t, err := s.execute(ctx, r, in)
		if err != nil {
			return err
		}
		result = &TransferResult{Transfer: t}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// replay returns the outcome of the request that originally used key. The same
// key with a different payload is a client bug and is rejected rather than
// silently answered with an unrelated result.
func (s *TransferService) replay(ctx context.Context, r repository.Repositories, key, hash string) (*TransferResult, error) {
	rec, err := r.Idempotency().Get(ctx, key)
	if err != nil {
		return nil, err
	}
	if rec == nil || rec.TransferID == nil {
		// Records are committed together with their transfer, so a visible
		// record without one means the data is corrupt.
		return nil, fmt.Errorf("idempotency key %q has no completed transfer", key)
	}
	if rec.RequestHash != hash {
		return nil, domain.ErrIdempotencyKeyReused
	}
	t, err := r.Transfers().Get(ctx, *rec.TransferID)
	if err != nil {
		return nil, err
	}
	return &TransferResult{Transfer: t, Replayed: true}, nil
}

// requestHash fingerprints the business content of a request (not its key), so
// a reused key can be told apart from a genuine retry.
func requestHash(in CreateTransferInput) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%d", in.FromWalletID, in.ToWalletID, in.Amount)))
	return hex.EncodeToString(sum[:])
}

// execute performs the transfer inside an open transaction.
func (s *TransferService) execute(ctx context.Context, r repository.Repositories, in CreateTransferInput) (*domain.Transfer, error) {
	// Lock both wallets (in a deterministic order) before reading balances so
	// the check-then-debit below cannot race with another transfer.
	wallets, err := r.Wallets().LockForUpdate(ctx, in.FromWalletID, in.ToWalletID)
	if err != nil {
		return nil, err
	}
	src, dst := wallets[in.FromWalletID], wallets[in.ToWalletID]
	if src == nil || dst == nil {
		// Nothing is written: the wallet may be created later, so a retry
		// should be evaluated afresh.
		return nil, domain.ErrWalletNotFound
	}

	now := s.now()
	t, err := domain.NewTransfer(s.newID(), in.IdempotencyKey, in.FromWalletID, in.ToWalletID, in.Amount, now)
	if err != nil {
		return nil, err
	}
	if err := r.Transfers().Insert(ctx, t); err != nil {
		return nil, err
	}
	if err := r.Idempotency().AttachTransfer(ctx, in.IdempotencyKey, t.ID); err != nil {
		return nil, err
	}

	reason, err := applyToWallets(src, dst, in.Amount)
	if err != nil {
		return nil, err
	}
	if reason != "" {
		if err := t.MarkFailed(reason, now); err != nil {
			return nil, err
		}
		if err := r.Transfers().SaveTransition(ctx, t, domain.TransferPending); err != nil {
			return nil, err
		}
		return t, nil
	}

	if err := r.Wallets().UpdateBalance(ctx, src, now); err != nil {
		return nil, err
	}
	if err := r.Wallets().UpdateBalance(ctx, dst, now); err != nil {
		return nil, err
	}
	if err := r.Ledger().Insert(ctx, domain.LedgerEntriesFor(t, now)...); err != nil {
		return nil, err
	}
	if err := t.MarkProcessed(now); err != nil {
		return nil, err
	}
	if err := r.Transfers().SaveTransition(ctx, t, domain.TransferPending); err != nil {
		return nil, err
	}
	return t, nil
}

// applyToWallets debits src and credits dst in memory. It returns a non-empty
// FailureReason for a business failure (the wallets are then discarded, never
// persisted) and a non-nil error only for unexpected problems.
func applyToWallets(src, dst *domain.Wallet, amount int64) (domain.FailureReason, error) {
	if err := src.Debit(amount); err != nil {
		return failureReasonFor(err)
	}
	if err := dst.Credit(amount); err != nil {
		return failureReasonFor(err)
	}
	return "", nil
}

func failureReasonFor(err error) (domain.FailureReason, error) {
	switch {
	case errors.Is(err, domain.ErrInsufficientFunds):
		return domain.FailureInsufficientFunds, nil
	case errors.Is(err, domain.ErrBalanceOverflow):
		return domain.FailureBalanceOverflow, nil
	default:
		return "", err
	}
}

// GetTransfer returns a transfer and its ledger entries.
func (s *TransferService) GetTransfer(ctx context.Context, id uuid.UUID) (*TransferDetails, error) {
	t, err := s.store.Transfers().Get(ctx, id)
	if err != nil {
		return nil, err
	}
	entries, err := s.store.Ledger().ListByTransfer(ctx, id)
	if err != nil {
		return nil, err
	}
	return &TransferDetails{Transfer: t, Entries: entries}, nil
}
