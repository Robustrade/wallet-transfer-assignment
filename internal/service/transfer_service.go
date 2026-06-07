// Package service contains the application business logic: the transfer
// workflow, idempotency handling and orchestration across repositories. It
// depends only on the repository interfaces, never on a concrete database.
package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
	"github.com/Robustrade/wallet-transfer-assignment/internal/repository"
	"github.com/google/uuid"
)

// IDGenerator produces unique transfer identifiers. It is injected so tests can
// supply deterministic ids.
type IDGenerator func() string

// TransferService implements the wallet transfer use cases.
type TransferService struct {
	store  repository.Store
	newID  IDGenerator
	logger *slog.Logger
}

// NewTransferService wires a service with its dependencies. A nil idGen falls
// back to UUIDv4 and a nil logger to slog.Default, so callers can pass only
// what they need.
func NewTransferService(store repository.Store, idGen IDGenerator, logger *slog.Logger) *TransferService {
	if idGen == nil {
		idGen = func() string { return uuid.NewString() }
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &TransferService{store: store, newID: idGen, logger: logger}
}

// CreateTransferResult is the outcome of a CreateTransfer call. Replayed
// reports whether the result came from a previously stored transfer (a
// duplicate idempotency key) rather than a freshly processed one, letting the
// transport layer signal 200 vs 201.
type CreateTransferResult struct {
	Transfer *domain.Transfer
	Replayed bool
}

// CreateTransfer executes a wallet-to-wallet transfer with exactly-once
// semantics for a given idempotency key.
//
// Workflow:
//  1. Validate the request (stateless invariants).
//  2. Inside one transaction, idempotently insert a PENDING transfer. A
//     duplicate key short-circuits and replays the stored result.
//  3. Lock both wallets in a deterministic order to avoid deadlocks.
//  4. Apply the debit/credit, write the balanced ledger pair and mark the
//     transfer PROCESSED — or mark it FAILED on a business error.
//
// The commit is atomic: either the balances, ledger and transfer status all
// move together, or nothing changes.
func (s *TransferService) CreateTransfer(ctx context.Context, req domain.TransferRequest) (*CreateTransferResult, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	result := &CreateTransferResult{}
	err := s.store.WithinTx(ctx, func(ctx context.Context, repos repository.Repositories) error {
		// Step 1: idempotent claim. The unique constraint on idempotency_key is
		// the single source of truth for deduplication.
		candidate := domain.NewTransfer(s.newID(), req.IdempotencyKey, req.FromWalletID, req.ToWalletID, req.Amount)
		inserted, existing, err := repos.Transfers.InsertIfAbsent(ctx, candidate)
		if err != nil {
			return err
		}
		if !inserted {
			// Duplicate request: replay the original outcome instead of
			// performing the transfer again.
			if !existing.SameParams(candidate) {
				return domain.ErrIdempotencyKeyReused
			}
			s.logger.InfoContext(ctx, "idempotent replay of existing transfer",
				slog.String("transfer_id", existing.ID),
				slog.String("idempotency_key", req.IdempotencyKey),
				slog.String("status", string(existing.Status)),
			)
			result.Transfer = existing
			result.Replayed = true
			return nil
		}

		// Step 2: process the freshly created PENDING transfer.
		processed, err := s.processTransfer(ctx, repos, candidate)
		if err != nil {
			return err
		}
		result.Transfer = processed
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// processTransfer applies a PENDING transfer to wallet balances and the ledger
// within an existing transaction. A business failure (insufficient funds,
// currency mismatch) is recorded as a FAILED transfer and returned without an
// error so the surrounding transaction still commits the FAILED state.
func (s *TransferService) processTransfer(ctx context.Context, repos repository.Repositories, t *domain.Transfer) (*domain.Transfer, error) {
	// Lock both wallets up front in a deterministic (sorted) order. Acquiring
	// locks in a global order prevents deadlocks when two transfers touch the
	// same pair of wallets in opposite directions.
	first, second := orderedLockIDs(t.FromWalletID, t.ToWalletID)
	locked := make(map[string]*domain.Wallet, 2)
	for _, id := range []string{first, second} {
		w, err := repos.Wallets.GetForUpdate(ctx, id)
		if err != nil {
			return nil, err
		}
		locked[id] = w
	}
	source := locked[t.FromWalletID]
	dest := locked[t.ToWalletID]

	// Business rule: both wallets must share a currency.
	if source.Currency != dest.Currency {
		return s.failTransfer(ctx, repos, t, domain.ReasonCurrencyMismatch)
	}

	// Business rule: the source must be able to cover the debit. This is a
	// FAILED outcome, not a transport error.
	if err := source.Debit(t.Amount); err != nil {
		if errors.Is(err, domain.ErrInsufficientFunds) {
			return s.failTransfer(ctx, repos, t, domain.ReasonInsufficientFunds)
		}
		return nil, err
	}
	if err := dest.Credit(t.Amount); err != nil {
		return nil, err
	}

	// Persist the new balances under the held locks.
	if err := repos.Wallets.UpdateBalance(ctx, source.ID, source.Balance); err != nil {
		return nil, err
	}
	if err := repos.Wallets.UpdateBalance(ctx, dest.ID, dest.Balance); err != nil {
		return nil, err
	}

	// Write the balanced double-entry pair.
	t.Currency = source.Currency
	debit, credit := domain.NewLedgerPair(t.ID, t.FromWalletID, t.ToWalletID, t.Amount)
	if err := repos.Ledger.InsertPair(ctx, &debit, &credit); err != nil {
		return nil, err
	}

	// Advance the state machine and persist the terminal status.
	if err := t.MarkProcessed(); err != nil {
		return nil, fmt.Errorf("mark processed: %w", err)
	}
	if err := repos.Transfers.UpdateStatus(ctx, t); err != nil {
		return nil, err
	}

	s.logger.InfoContext(ctx, "transfer processed",
		slog.String("transfer_id", t.ID),
		slog.String("from_wallet", t.FromWalletID),
		slog.String("to_wallet", t.ToWalletID),
		slog.Int64("amount", t.Amount),
	)
	return t, nil
}

// failTransfer records a business failure as a FAILED transfer. No ledger
// entries are written, so the ledger always stays balanced. The error from the
// caller is intentionally swallowed into the transfer state and the transaction
// commits the FAILED status.
func (s *TransferService) failTransfer(ctx context.Context, repos repository.Repositories, t *domain.Transfer, reason string) (*domain.Transfer, error) {
	if err := t.MarkFailed(reason); err != nil {
		return nil, fmt.Errorf("mark failed: %w", err)
	}
	if err := repos.Transfers.UpdateStatus(ctx, t); err != nil {
		return nil, err
	}
	s.logger.WarnContext(ctx, "transfer failed",
		slog.String("transfer_id", t.ID),
		slog.String("reason", reason),
	)
	return t, nil
}

// GetTransfer returns a transfer by id for status polling.
func (s *TransferService) GetTransfer(ctx context.Context, id string) (*domain.Transfer, error) {
	return s.store.Reader().Transfers.Get(ctx, id)
}

// orderedLockIDs returns the two wallet ids sorted, giving every transaction a
// single global lock-acquisition order.
func orderedLockIDs(a, b string) (first, second string) {
	ids := []string{a, b}
	sort.Strings(ids)
	return ids[0], ids[1]
}
