package service

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"wallet-transfer-assignment/internal/domain"
	"wallet-transfer-assignment/internal/repository"
)

type TransferService struct {
	txManager repository.TransactionManager
	transfers repository.TransferRepository
}

func NewTransferService(
	txManager repository.TransactionManager,
	transfers repository.TransferRepository,
) *TransferService {
	return &TransferService{
		txManager: txManager,
		transfers: transfers,
	}
}

type CreateTransferResult struct {
	Transfer *domain.Transfer
	Replayed bool
}

func (s *TransferService) CreateTransfer(
	ctx context.Context,
	req domain.CreateTransfer,
) (*CreateTransferResult, error) {
	if err := validateTransfer(req); err != nil {
		return nil, err
	}

	// Fast path for a normal retry.
	existing, err := s.transfers.FindByIdempotencyKey(
		ctx,
		req.IdempotencyKey,
	)

	if err == nil {
		return resultFromExisting(existing, req)
	}

	if !errors.Is(err, repository.ErrNotFound) {
		return nil, err
	}

	// Nothing currently exists, so attempt execution.
	return s.executeTransfer(ctx, req)
}

func validateTransfer(req domain.CreateTransfer) error {
	if req.IdempotencyKey == "" {
		return domain.ErrIdempotencyKeyRequired
	}

	if req.Amount <= 0 {
		return domain.ErrInvalidAmount
	}

	if req.FromWalletID == uuid.Nil ||
		req.ToWalletID == uuid.Nil {
		return domain.ErrWalletNotFound
	}

	if req.FromWalletID == req.ToWalletID {
		return domain.ErrSameWallet
	}

	return nil
}

func (s *TransferService) executeTransfer(ctx context.Context, req domain.CreateTransfer) (*CreateTransferResult, error) {

	t := &domain.Transfer{
		ID:             uuid.New(),
		IdempotencyKey: req.IdempotencyKey,
		FromWalletID:   req.FromWalletID,
		ToWalletID:     req.ToWalletID,
		Amount:         req.Amount,
		Status:         domain.TransferStatusPending,
		FailureReason:  domain.TransferFailureNone,
	}

	var businessErr error

	err := s.txManager.WithTx(
		ctx,
		func(repos repository.TxRepositories) error {

			wallets, err := repos.Wallets().
				FindByIDsForUpdate(
					ctx,
					req.FromWalletID,
					req.ToWalletID,
				)

			if err != nil {
				if errors.Is(err, repository.ErrNotFound) {
					return domain.ErrWalletNotFound
				}

				return err
			}

			if len(wallets) != 2 {
				return domain.ErrWalletNotFound
			}

			source := findWallet(
				wallets,
				req.FromWalletID,
			)

			if source == nil {
				return domain.ErrWalletNotFound
			}

			if err := repos.Transfers().Create(ctx, t); err != nil {
				return err
			}

			if source.Balance < req.Amount {
				if err := markTransferFailed(
					ctx,
					repos.Transfers(),
					t,
					domain.TransferFailureInsufficientFunds,
				); err != nil {
					return err
				}

				businessErr = domain.ErrInsufficientFunds
				return nil
			}

			if err := repos.Wallets().Debit(
				ctx,
				req.FromWalletID,
				req.Amount,
			); err != nil {
				// Because Debit also has balance >= amount protection,
				// handle an unexpected insufficient-funds result safely.
				if errors.Is(
					err,
					domain.ErrInsufficientFunds,
				) {
					if err := markTransferFailed(
						ctx,
						repos.Transfers(),
						t,
						domain.TransferFailureInsufficientFunds,
					); err != nil {
						return err
					}

					businessErr = domain.ErrInsufficientFunds
					return nil
				}

				return err
			}

			if err := repos.Wallets().Credit(
				ctx,
				req.ToWalletID,
				req.Amount,
			); err != nil {
				return err
			}

			debit := &domain.LedgerEntry{
				ID:         uuid.New(),
				TransferID: t.ID,
				WalletID:   req.FromWalletID,
				Type:       domain.LedgerEntryTypeDebit,
				Amount:     req.Amount,
			}

			credit := &domain.LedgerEntry{
				ID:         uuid.New(),
				TransferID: t.ID,
				WalletID:   req.ToWalletID,
				Type:       domain.LedgerEntryTypeCredit,
				Amount:     req.Amount,
			}

			if err := repos.Ledger().CreateMany(
				ctx,
				debit,
				credit,
			); err != nil {
				return err
			}

			if err := repos.Transfers().UpdateStatus(
				ctx,
				t.ID,
				domain.TransferStatusProcessed,
				domain.TransferFailureNone,
			); err != nil {
				return err
			}

			t.Status = domain.TransferStatusProcessed
			t.FailureReason = domain.TransferFailureNone

			return nil
		},
	)

	if err != nil {
		return s.handleExecutionError(
			ctx,
			req,
			err,
		)
	}

	return &CreateTransferResult{
		Transfer: t,
		Replayed: false,
	}, businessErr
}

func markTransferFailed(ctx context.Context, repo repository.TransferRepository, t *domain.Transfer, reason domain.TransferFailureReason) error {

	if err := repo.UpdateStatus(
		ctx,
		t.ID,
		domain.TransferStatusFailed,
		reason,
	); err != nil {
		return err
	}

	t.Status = domain.TransferStatusFailed
	t.FailureReason = reason

	return nil
}

func findWallet(wallets []*domain.Wallet, id uuid.UUID) *domain.Wallet {

	for _, w := range wallets {
		if w.ID == id {
			return w
		}
	}

	return nil
}

func resultFromExisting(existing *domain.Transfer, req domain.CreateTransfer) (*CreateTransferResult, error) {

	// Same idempotency key must describe the exact same request.
	if existing.FromWalletID != req.FromWalletID ||
		existing.ToWalletID != req.ToWalletID ||
		existing.Amount != req.Amount {

		return nil, domain.ErrIdempotencyConflict
	}

	result := &CreateTransferResult{
		Transfer: existing,
		Replayed: true,
	}

	switch existing.Status {

	case domain.TransferStatusProcessed:
		return result, nil

	case domain.TransferStatusFailed:
		switch existing.FailureReason {

		case domain.TransferFailureInsufficientFunds:
			return result, domain.ErrInsufficientFunds

		case domain.TransferFailureWalletNotFound:
			return result, domain.ErrWalletNotFound

		default:
			return result, domain.ErrInvalidTransferState
		}

	case domain.TransferStatusPending:
		// With our current transaction model a committed PENDING
		// state should normally not be visible.
		return result, domain.ErrInvalidTransferState

	default:
		return result, domain.ErrInvalidTransferState
	}
}

func (s *TransferService) handleExecutionError(ctx context.Context, req domain.CreateTransfer, executionErr error) (*CreateTransferResult, error) {

	if !errors.Is(
		executionErr,
		repository.ErrAlreadyExists,
	) {
		return nil, executionErr
	}

	// Our transaction was rolled back because another request
	// already inserted the same idempotency key.
	//
	// Query using the normal, non-transactional repository.
	existing, err := s.transfers.FindByIdempotencyKey(
		ctx,
		req.IdempotencyKey,
	)

	if err != nil {
		return nil, err
	}

	return resultFromExisting(
		existing,
		req,
	)
}
