package service

import (
	"context"
	"errors"
	"sort"

	"github.com/malem-mayeng/wallet-transfer-assignment/internal/domain"
	"github.com/malem-mayeng/wallet-transfer-assignment/internal/repository"
)

type CreateTransferRequest struct {
	IdempotencyKey string
	FromWalletID   string
	ToWalletID     string
	Amount         int64
}

type TransferService struct {
	repo repository.Repository
}

func NewTransferService(repo repository.Repository) *TransferService {
	return &TransferService{
		repo: repo,
	}
}

func (s *TransferService) CreateTransfer(
	ctx context.Context,
	req CreateTransferRequest,
) (*domain.Transfer, error) {
	if req.IdempotencyKey == "" {
		return nil, domain.ErrInvalidIdempotencyKey
	}

	existingTransfer, err := s.repo.GetTransferByIdempotencyKey(
		ctx,
		req.IdempotencyKey,
	)

	switch {
	case err == nil:
		if existingTransfer.FromWalletID != req.FromWalletID ||
			existingTransfer.ToWalletID != req.ToWalletID ||
			existingTransfer.Amount != req.Amount {
			return nil, domain.ErrIdempotencyConflict
		}

		return existingTransfer, nil

	case !errors.Is(err, domain.ErrTransferNotFound):
		return nil, err
	}

	if req.FromWalletID == "" || req.ToWalletID == "" {
		return nil, domain.ErrInvalidWalletID
	}

	if req.Amount <= 0 {
		return nil, domain.ErrInvalidAmount
	}

	if req.FromWalletID == req.ToWalletID {
		return nil, domain.ErrSameWallet
	}
	var result *domain.Transfer

	err = s.repo.WithTx(ctx, func(
		ctx context.Context,
		store repository.TransferStore,
	) error {
		walletIDs := []string{
			req.FromWalletID,
			req.ToWalletID,
		}

		sort.Strings(walletIDs)

		wallets := make(map[string]*domain.Wallet, 2)

		// Lock both wallets before inserting the transfer.
		// Always acquire locks in deterministic order to avoid
		// deadlocks between opposite-direction transfers.
		for _, walletID := range walletIDs {
			wallet, err := store.GetWalletForUpdate(ctx, walletID)
			if err != nil {
				return err
			}

			wallets[walletID] = wallet
		}

		transfer := &domain.Transfer{
			IdempotencyKey: req.IdempotencyKey,
			FromWalletID:   req.FromWalletID,
			ToWalletID:     req.ToWalletID,
			Amount:         req.Amount,
			Status:         domain.TransferPending,
		}

		existingTransfer, created, err := store.CreateOrGetTransfer(
			ctx,
			transfer,
		)
		if err != nil {
			return err
		}

		if !created {
			if existingTransfer.FromWalletID != req.FromWalletID ||
				existingTransfer.ToWalletID != req.ToWalletID ||
				existingTransfer.Amount != req.Amount {
				return domain.ErrIdempotencyConflict
			}

			result = existingTransfer
			return nil
		}

		fromWallet := wallets[req.FromWalletID]
		toWallet := wallets[req.ToWalletID]

		if fromWallet.Balance < req.Amount {
			if err := store.UpdateTransferStatus(
				ctx,
				existingTransfer.ID,
				domain.TransferFailed,
			); err != nil {
				return err
			}

			existingTransfer.Status = domain.TransferFailed
			result = existingTransfer

			return nil
		}

		fromWallet.Balance -= req.Amount
		toWallet.Balance += req.Amount

		if err := store.UpdateWalletBalance(
			ctx,
			fromWallet.ID,
			fromWallet.Balance,
		); err != nil {
			return err
		}

		if err := store.UpdateWalletBalance(
			ctx,
			toWallet.ID,
			toWallet.Balance,
		); err != nil {
			return err
		}

		debitEntry := &domain.LedgerEntry{
			TransferID: existingTransfer.ID,
			WalletID:   fromWallet.ID,
			EntryType:  domain.LedgerDebit,
			Amount:     req.Amount,
		}

		if err := store.CreateLedgerEntry(ctx, debitEntry); err != nil {
			return err
		}

		creditEntry := &domain.LedgerEntry{
			TransferID: existingTransfer.ID,
			WalletID:   toWallet.ID,
			EntryType:  domain.LedgerCredit,
			Amount:     req.Amount,
		}

		if err := store.CreateLedgerEntry(ctx, creditEntry); err != nil {
			return err
		}

		if err := store.UpdateTransferStatus(
			ctx,
			existingTransfer.ID,
			domain.TransferProcessed,
		); err != nil {
			return err
		}

		existingTransfer.Status = domain.TransferProcessed
		result = existingTransfer

		return nil
	})

	if err != nil {
		return nil, err
	}

	return result, nil
}
