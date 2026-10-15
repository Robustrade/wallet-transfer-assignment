package service

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/RKShukla1997/wallet-transfer-assignment/internal/domain"
	"github.com/RKShukla1997/wallet-transfer-assignment/internal/repository"
)

// TransferService orchestrates the wallet transfer workflow: idempotency
// handling, transaction boundaries, wallet locking, balance validation,
// ledger generation, and transfer state transitions. This is the only
// layer that encodes business rules; handlers and repositories do not.
type TransferService struct {
	pool         *pgxpool.Pool
	walletRepo   *repository.WalletRepository
	transferRepo *repository.TransferRepository
	ledgerRepo   *repository.LedgerRepository
}

func NewTransferService(
	pool *pgxpool.Pool,
	walletRepo *repository.WalletRepository,
	transferRepo *repository.TransferRepository,
	ledgerRepo *repository.LedgerRepository,
) *TransferService {
	return &TransferService{
		pool:         pool,
		walletRepo:   walletRepo,
		transferRepo: transferRepo,
		ledgerRepo:   ledgerRepo,
	}
}

// CreateTransferInput is the sanitized request the handler passes to the
// service. Structural validation (required fields, JSON shape) happens in
// the handler; business validation happens here.
type CreateTransferInput struct {
	IdempotencyKey string
	FromWalletID   string
	ToWalletID     string
	Amount         int64
}

// CreateTransfer executes (or replays) a wallet-to-wallet transfer.
//
// Flow (all within one transaction):
//  1. Lock both wallets in a stable order (see the comment below for why
//     this must happen before the transfer row is inserted).
//  2. Insert a PENDING transfer row guarded by the unique idempotency key.
//     If a row with this key already exists, the existing transfer's
//     terminal result is returned without re-executing any side effects.
//  3. Otherwise, validate sufficient funds, then either move the balances
//     and write the double-entry ledger (-> PROCESSED) or mark the
//     transfer FAILED with no balance/ledger mutation.
func (s *TransferService) CreateTransfer(ctx context.Context, in CreateTransferInput) (domain.Transfer, error) {
	candidate := domain.Transfer{
		IdempotencyKey: in.IdempotencyKey,
		FromWalletID:   in.FromWalletID,
		ToWalletID:     in.ToWalletID,
		Amount:         in.Amount,
	}
	if err := candidate.Validate(); err != nil {
		return domain.Transfer{}, err
	}

	var result domain.Transfer

	err := s.withTx(ctx, func(tx pgx.Tx) error {
		// Lock both wallets (sorted by id) before inserting the transfer
		// row. Locking after the insert deadlocks under concurrency: the
		// insert's implicit FK lock on the wallet rows collides with the
		// explicit FOR UPDATE lock requested afterwards. See "Design
		// Decisions" in solution.md for how this was found.
		fromWallet, toWallet, err := s.walletRepo.LockTwoForUpdate(ctx, tx, candidate.FromWalletID, candidate.ToWalletID)
		if err != nil {
			return fmt.Errorf("locking wallets: %w", err)
		}

		inserted, isNew, err := s.transferRepo.InsertIfAbsent(ctx, tx, candidate)
		if err != nil {
			return fmt.Errorf("inserting transfer: %w", err)
		}

		if !isNew {
			// Duplicate request: idempotencyKey already recorded. Return
			// the original result verbatim; no side effects re-run.
			existing, err := s.transferRepo.GetByIdempotencyKey(ctx, tx, in.IdempotencyKey)
			if err != nil {
				return fmt.Errorf("fetching existing transfer: %w", err)
			}
			result = existing
			return nil
		}

		processed, err := s.execute(ctx, tx, inserted, fromWallet, toWallet)
		if err != nil {
			return err
		}
		result = processed
		return nil
	})
	if err != nil {
		return domain.Transfer{}, err
	}
	return result, nil
}

// execute runs the balance-moving part of the workflow for a freshly
// inserted PENDING transfer, within the caller's transaction. fromWallet
// and toWallet must already be locked (FOR UPDATE) by the caller.
func (s *TransferService) execute(ctx context.Context, tx pgx.Tx, t domain.Transfer, fromWallet, toWallet domain.Wallet) (domain.Transfer, error) {
	// Balance check happens after the row lock is held, so no concurrent
	// transaction can have changed the balance since this read.
	if fromWallet.Balance < t.Amount {
		return s.fail(ctx, tx, t, domain.ErrInsufficientFunds.Error())
	}

	if err := s.walletRepo.UpdateBalance(ctx, tx, fromWallet.ID, fromWallet.Balance-t.Amount); err != nil {
		return domain.Transfer{}, fmt.Errorf("debiting wallet: %w", err)
	}
	if err := s.walletRepo.UpdateBalance(ctx, tx, toWallet.ID, toWallet.Balance+t.Amount); err != nil {
		return domain.Transfer{}, fmt.Errorf("crediting wallet: %w", err)
	}

	if err := s.ledgerRepo.InsertDoubleEntry(ctx, tx, t.ID, t.FromWalletID, t.ToWalletID, t.Amount); err != nil {
		return domain.Transfer{}, fmt.Errorf("writing ledger entries: %w", err)
	}

	processed, err := s.transferRepo.MarkProcessed(ctx, tx, t.ID)
	if err != nil {
		return domain.Transfer{}, fmt.Errorf("marking transfer processed: %w", err)
	}
	return processed, nil
}

// fail transitions a transfer to FAILED. Insufficient funds and similar
// domain-level rejections are expected outcomes, not transaction errors:
// they are committed so a retry with the same idempotency key replays the
// same FAILED result instead of re-evaluating balances.
func (s *TransferService) fail(ctx context.Context, tx pgx.Tx, t domain.Transfer, reason string) (domain.Transfer, error) {
	failed, err := s.transferRepo.MarkFailed(ctx, tx, t.ID, reason)
	if err != nil {
		return domain.Transfer{}, fmt.Errorf("marking transfer failed: %w", err)
	}
	return failed, nil
}

// GetTransfer fetches a transfer by id, e.g. for a transfer-history API.
func (s *TransferService) GetTransfer(ctx context.Context, id string) (domain.Transfer, error) {
	var result domain.Transfer
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		t, err := s.transferRepo.GetByID(ctx, tx, id)
		if err != nil {
			return err
		}
		result = t
		return nil
	})
	return result, err
}

func (s *TransferService) withTx(ctx context.Context, fn func(tx pgx.Tx) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op if already committed

	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("committing transaction: %w", err)
	}
	return nil
}
