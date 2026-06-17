package service

import (
	"context"
	"errors"
	"sort"

	"github.com/GowthamMuddusetty/wallet-transfer-assignment/internal/domain"
	"github.com/GowthamMuddusetty/wallet-transfer-assignment/internal/repository"
)

// Service-level errors.
var (
	ErrInsufficientBalance = errors.New("insufficient balance")
)

// TransferRequest is the input to a transfer.
type TransferRequest struct {
	IdempotencyKey string
	FromWalletID   string
	ToWalletID     string
	Amount         int64
}

// TransferService orchestrates the transfer workflow.
type TransferService struct {
	txm repository.TxManager
}

// NewTransferService builds a service using the given tx manager.
func NewTransferService(txm repository.TxManager) *TransferService {
	return &TransferService{txm: txm}
}

// CreateTransfer executes a transfer with exactly-once semantics.
// The whole workflow runs in one transaction. Duplicate idempotency keys
// return the original transfer without moving money again.
func (s *TransferService) CreateTransfer(ctx context.Context, req TransferRequest) (*domain.Transfer, error) {
	// Pure validation before touching the DB.
	if err := domain.ValidateTransferRequest(req.FromWalletID, req.ToWalletID, req.Amount); err != nil {
		return nil, err
	}

	var result *domain.Transfer

	err := s.txm.WithTx(ctx, func(repo repository.Repository) error {
		// Fast path: key already processed -> return original.
		existingID, err := repo.GetTransferIDByIdempotencyKey(ctx, req.IdempotencyKey)
		if err == nil {
			tr, gErr := repo.GetTransferByID(ctx, existingID)
			if gErr != nil {
				return gErr
			}
			result = tr
			return nil
		}
		if !errors.Is(err, repository.ErrIdempotencyKeyNotFound) {
			return err
		}

		// Lock wallets in a consistent order to avoid deadlocks.
		if err := lockWalletsInOrder(ctx, repo, req.FromWalletID, req.ToWalletID); err != nil {
			return err
		}

		// Re-read source under lock to get authoritative balance.
		from, err := repo.GetWalletForUpdate(ctx, req.FromWalletID)
		if err != nil {
			return err
		}
		to, err := repo.GetWalletForUpdate(ctx, req.ToWalletID)
		if err != nil {
			return err
		}

		if from.Balance < req.Amount {
			return ErrInsufficientBalance
		}

		// Create the transfer (PENDING).
		tr, err := repo.CreateTransfer(ctx, &domain.Transfer{
			FromWalletID: req.FromWalletID,
			ToWalletID:   req.ToWalletID,
			Amount:       req.Amount,
			Status:       domain.StatusPending,
		})
		if err != nil {
			return err
		}

		// Move money.
		if err := repo.UpdateWalletBalance(ctx, from.ID, from.Balance-req.Amount); err != nil {
			return err
		}
		if err := repo.UpdateWalletBalance(ctx, to.ID, to.Balance+req.Amount); err != nil {
			return err
		}

		// Double-entry ledger.
		if err := repo.CreateLedgerEntry(ctx, &domain.LedgerEntry{
			WalletID:   from.ID,
			TransferID: tr.ID,
			Type:       domain.EntryDebit,
			Amount:     req.Amount,
		}); err != nil {
			return err
		}
		if err := repo.CreateLedgerEntry(ctx, &domain.LedgerEntry{
			WalletID:   to.ID,
			TransferID: tr.ID,
			Type:       domain.EntryCredit,
			Amount:     req.Amount,
		}); err != nil {
			return err
		}

		// Mark processed.
		if err := tr.TransitionTo(domain.StatusProcessed); err != nil {
			return err
		}
		if err := repo.UpdateTransferStatus(ctx, tr.ID, domain.StatusProcessed); err != nil {
			return err
		}

		// Persist idempotency record. A concurrent duplicate loses the race
		// here and we return that original transfer instead.
		if err := repo.CreateIdempotencyRecord(ctx, req.IdempotencyKey, tr.ID); err != nil {
			if errors.Is(err, repository.ErrDuplicateIdempotencyKey) {
				existingID, gErr := repo.GetTransferIDByIdempotencyKey(ctx, req.IdempotencyKey)
				if gErr != nil {
					return gErr
				}
				orig, gErr := repo.GetTransferByID(ctx, existingID)
				if gErr != nil {
					return gErr
				}
				result = orig
				return nil
			}
			return err
		}

		result = tr
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// lockWalletsInOrder locks both wallets sorted by ID to prevent deadlocks
// when two transfers touch the same pair in opposite directions.
func lockWalletsInOrder(ctx context.Context, repo repository.Repository, a, b string) error {
	ids := []string{a, b}
	sort.Strings(ids)
	for _, id := range ids {
		if _, err := repo.GetWalletForUpdate(ctx, id); err != nil {
			return err
		}
	}
	return nil
}