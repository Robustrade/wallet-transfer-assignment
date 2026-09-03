package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
	"github.com/Robustrade/wallet-transfer-assignment/internal/repository"
)

type TransferResult struct {
	Transfer *domain.Transfer
	Entries  []domain.LedgerEntry
	Replayed bool
}

func (s *Service) CreateTransfer(ctx context.Context, req domain.TransferRequest) (*TransferResult, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	transfer, replayed, err := s.reserve(ctx, req)
	if err != nil {
		return nil, err
	}

	if !transfer.Status.IsTerminal() {
		if transfer, err = s.settle(ctx, transfer.ID); err != nil {
			return nil, err
		}
	}

	entries, err := s.store.ListLedgerEntriesByTransfer(ctx, transfer.ID)
	if err != nil {
		return nil, err
	}
	return &TransferResult{Transfer: transfer, Entries: entries, Replayed: replayed}, nil
}

func (s *Service) GetTransfer(ctx context.Context, id string) (*TransferResult, error) {
	transfer, err := s.store.GetTransfer(ctx, id)
	if err != nil {
		return nil, err
	}
	entries, err := s.store.ListLedgerEntriesByTransfer(ctx, id)
	if err != nil {
		return nil, err
	}
	return &TransferResult{Transfer: transfer, Entries: entries}, nil
}

func (s *Service) reserve(ctx context.Context, req domain.TransferRequest) (*domain.Transfer, bool, error) {
	timestamp := now()
	transfer := domain.NewTransfer(req, timestamp)

	err := s.store.WithTx(ctx, func(tx *repository.Tx) error {
		if _, err := tx.LockWallets(ctx, req.FromWalletID, req.ToWalletID); err != nil {
			return err
		}
		if err := tx.CreateTransfer(ctx, transfer); err != nil {
			return err
		}
		return tx.CreateIdempotencyRecord(ctx, &domain.IdempotencyRecord{
			Key:         req.IdempotencyKey,
			RequestHash: req.Hash(),
			TransferID:  transfer.ID,
			CreatedAt:   timestamp,
		})
	})
	switch {
	case err == nil:
		return transfer, false, nil
	case errors.Is(err, domain.ErrDuplicateKey):
		existing, err := s.replay(ctx, req)
		return existing, true, err
	default:
		return nil, false, err
	}
}

func (s *Service) replay(ctx context.Context, req domain.TransferRequest) (*domain.Transfer, error) {
	record, err := s.store.GetIdempotencyRecord(ctx, req.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	if record == nil {
		return nil, fmt.Errorf("%w: %s", domain.ErrDuplicateKey, req.IdempotencyKey)
	}
	if record.RequestHash != req.Hash() {
		return nil, fmt.Errorf("%w: %s", domain.ErrIdempotencyConflict, req.IdempotencyKey)
	}
	return s.store.GetTransfer(ctx, record.TransferID)
}

func (s *Service) settle(ctx context.Context, transferID string) (*domain.Transfer, error) {
	var settled *domain.Transfer

	err := s.store.WithTx(ctx, func(tx *repository.Tx) error {
		transfer, err := tx.LockTransfer(ctx, transferID)
		if err != nil {
			return err
		}
		if transfer.Status.IsTerminal() {
			settled = transfer
			return nil
		}

		timestamp := now()
		wallets, err := tx.LockWallets(ctx, transfer.FromWalletID, transfer.ToWalletID)
		if err != nil {
			return err
		}

		if wallets[transfer.FromWalletID].Balance < transfer.Amount {
			settled, err = markSettled(ctx, tx, transfer, domain.StatusFailed, domain.FailureInsufficientFunds, timestamp)
			return err
		}

		debit, credit := domain.NewLedgerPair(transfer, timestamp)
		if err := tx.InsertLedgerPair(ctx, debit, credit); err != nil {
			return err
		}
		if err := tx.AdjustWalletBalance(ctx, transfer.FromWalletID, -transfer.Amount, timestamp); err != nil {
			return err
		}
		if err := tx.AdjustWalletBalance(ctx, transfer.ToWalletID, transfer.Amount, timestamp); err != nil {
			return err
		}
		settled, err = markSettled(ctx, tx, transfer, domain.StatusProcessed, "", timestamp)
		return err
	})
	if err != nil {
		return nil, err
	}
	return settled, nil
}

func markSettled(ctx context.Context, tx *repository.Tx, transfer *domain.Transfer,
	status domain.TransferStatus, failureReason string, timestamp time.Time) (*domain.Transfer, error) {
	changed, err := tx.SettleTransfer(ctx, transfer.ID, status, failureReason, timestamp)
	if err != nil {
		return nil, err
	}
	if !changed {
		return nil, fmt.Errorf("transfer %s left PENDING while locked for settlement", transfer.ID)
	}
	transfer.Status = status
	transfer.FailureReason = failureReason
	transfer.UpdatedAt = timestamp
	return transfer, nil
}
