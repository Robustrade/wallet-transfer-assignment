package service

// Transfer lifecycle
// ──────────────────
//
//  1. Validate the request (positive amount, distinct wallets, non-empty idempotency key).
//  2. Persist a PENDING transfer record using an INSERT … ON CONFLICT DO NOTHING on the
//     idempotency_key column.  If the key already exists, the existing record is returned
//     immediately — no side effects are replayed.
//  3. Open a single database transaction and, within it:
//     a. Lock both wallet rows FOR UPDATE in sorted ID order to prevent deadlocks.
//     b. Verify the source wallet has sufficient balance.
//     c. Debit the source wallet and credit the destination wallet.
//     d. Insert two ledger entries (DEBIT + CREDIT).
//     e. Transition the transfer to PROCESSED.
//     f. Commit.
//  4. If the transaction fails for any reason (insufficient funds, wallet missing, DB error)
//     mark the transfer as FAILED with the error reason and return the failed transfer.
//
// Because step 3e is inside the same transaction as the balance changes, the PROCESSED
// status is only visible to readers after the balances have moved — never before.

import (
	"context"
	"sort"

	"wallet-transfer/internal/domain"

	"github.com/google/uuid"
)

// CreateTransfer executes a wallet-to-wallet transfer with exactly-once semantics.
//
// On success it returns a PROCESSED transfer and a nil error.
// On business failure (e.g. insufficient funds) it returns a FAILED transfer
// and the underlying error so callers can choose an appropriate HTTP status.
// On a duplicate idempotency key it returns the previously recorded transfer
// without performing any side effects.
func (s *Service) CreateTransfer(ctx context.Context, req CreateTransferRequest) (*domain.Transfer, error) {
	// ── Input validation ──────────────────────────────────────────────────
	if req.Amount <= 0 {
		return nil, domain.ErrInvalidAmount
	}
	if req.FromWalletID == req.ToWalletID {
		return nil, domain.ErrSameWallet
	}
	if req.IdempotencyKey == "" {
		return nil, domain.ErrMissingIdempotencyKey
	}

	// ── Idempotency guard ─────────────────────────────────────────────────
	// Attempt to insert a PENDING transfer record.  The UNIQUE constraint on
	// idempotency_key guarantees that a concurrent or duplicate request will
	// receive the already-persisted record instead of creating a second one.
	pending := &domain.Transfer{
		ID:             uuid.New().String(),
		IdempotencyKey: req.IdempotencyKey,
		FromWalletID:   req.FromWalletID,
		ToWalletID:     req.ToWalletID,
		Amount:         req.Amount,
		Status:         domain.StatusPending,
	}

	transfer, isNew, err := s.transferRepo.CreateIfNotExists(ctx, s.pool, pending)
	if err != nil {
		return nil, err
	}
	if !isNew {
		// Duplicate request — return the original result, no side effects.
		return transfer, nil
	}

	// ── Execute transfer ──────────────────────────────────────────────────
	if err := s.processTransfer(ctx, transfer); err != nil {
		reason := err.Error()
		// Best-effort status update; if this also fails the transfer stays
		// PENDING. A reconciliation job would handle stuck records in production.
		_ = s.transferRepo.UpdateStatus(ctx, s.pool, transfer.ID, domain.StatusFailed, reason)
		transfer.Status = domain.StatusFailed
		return transfer, err
	}

	transfer.Status = domain.StatusProcessed
	return transfer, nil
}

// processTransfer executes the atomic transfer within a single database
// transaction: lock wallets → validate balance → update balances →
// write ledger entries → mark transfer PROCESSED → commit.
func (s *Service) processTransfer(ctx context.Context, transfer *domain.Transfer) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback on any non-commit path

	// Lock both wallet rows in sorted ID order.
	// Acquiring locks in a consistent global order prevents two concurrent
	// transfers from deadlocking when each holds one lock and waits for the other.
	lockOrder := []string{transfer.FromWalletID, transfer.ToWalletID}
	sort.Strings(lockOrder)

	wallets := make(map[string]*domain.Wallet, 2)
	for _, id := range lockOrder {
		w, err := s.walletRepo.GetByIDForUpdate(ctx, tx, id)
		if err != nil {
			return err
		}
		wallets[id] = w
	}

	// Validate balance after holding the lock so no concurrent debit can
	// race between the read and the write.
	if wallets[transfer.FromWalletID].Balance < transfer.Amount {
		return domain.ErrInsufficientFunds
	}

	if err := s.walletRepo.UpdateBalance(ctx, tx, transfer.FromWalletID, -transfer.Amount); err != nil {
		return err
	}
	if err := s.walletRepo.UpdateBalance(ctx, tx, transfer.ToWalletID, transfer.Amount); err != nil {
		return err
	}

	// Double-entry ledger: one DEBIT and one CREDIT for every transfer.
	entries := []domain.LedgerEntry{
		{TransferID: transfer.ID, WalletID: transfer.FromWalletID, Type: domain.EntryDebit, Amount: transfer.Amount},
		{TransferID: transfer.ID, WalletID: transfer.ToWalletID, Type: domain.EntryCredit, Amount: transfer.Amount},
	}
	if err := s.ledgerRepo.CreateEntries(ctx, tx, entries); err != nil {
		return err
	}

	// Transition to PROCESSED inside the same transaction so the status change
	// is atomic with the balance updates.  If the commit fails, the DB rolls
	// back to PENDING — money never moved, no inconsistency.
	if err := s.transferRepo.UpdateStatus(ctx, tx, transfer.ID, domain.StatusProcessed, ""); err != nil {
		return err
	}

	return tx.Commit(ctx)
}
