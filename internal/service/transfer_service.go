package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/trjade/wallet-transfer-service/internal/domain"
	"github.com/trjade/wallet-transfer-service/internal/dto"
	"github.com/trjade/wallet-transfer-service/internal/repository"
	"github.com/trjade/wallet-transfer-service/internal/repository/postgres"
)

type TransferService struct {
	txManager    repository.TxManager
	walletRepo   repository.WalletRepository
	transferRepo repository.TransferRepository
	ledgerRepo   repository.LedgerRepository
}

func NewTransferService(txManager repository.TxManager, walletRepo repository.WalletRepository, transferRepo repository.TransferRepository, ledgerRepo repository.LedgerRepository) *TransferService {
	return &TransferService{
		txManager:    txManager,
		walletRepo:   walletRepo,
		transferRepo: transferRepo,
		ledgerRepo:   ledgerRepo,
	}
}

func (s *TransferService) CreateTransfer(ctx context.Context, req dto.CreateTransferRequest) (*domain.Transfer, error) {
	if req.IdempotencyKey == "" {
		return nil, domain.ErrInvalidIdempotencyKey
	}

	// Check if transfer with the same idempotency key already exists
	existingTransfer, err := s.transferRepo.GetTransferByIdempotencyKey(ctx, req.IdempotencyKey)
	if err != nil && !errors.Is(err, domain.ErrTransferNotFound) {
		return nil, fmt.Errorf("failed to check existing transfer: %w", err)
	}
	if existingTransfer != nil {
		return existingTransfer, nil
	}

	if req.FromWalletID == req.ToWalletID {
		return nil, domain.ErrSameWalletTransfer
	}

	var createdTransfer *domain.Transfer
	err = s.txManager.WithTx(ctx, func(txCtx context.Context) error {
		// Lock wallets in a consistent order to prevent deadlocks
		wallets, err := s.walletRepo.GetWalletsForUpdate(txCtx, []uuid.UUID{req.FromWalletID, req.ToWalletID})
		if err != nil {
			return fmt.Errorf("failed to lock wallets: %w", err)
		}

		// extract wallets
		var fromWallet, toWallet *domain.Wallet
		for i := range wallets {
			if wallets[i].ID == req.FromWalletID {
				fromWallet = &wallets[i]
			} else if wallets[i].ID == req.ToWalletID {
				toWallet = &wallets[i]
			}
		}
		if fromWallet == nil || toWallet == nil {
			return fmt.Errorf("one or both wallets not found")
		}

		// Check if fromWallet has sufficient balance
		if fromWallet.Balance < req.Amount {
			return domain.ErrInsufficientBalance
		}

		// Create transfer record
		transfer := &domain.Transfer{
			ID:             uuid.New(),
			IdempotencyKey: req.IdempotencyKey,
			FromWalletID:   req.FromWalletID,
			ToWalletID:     req.ToWalletID,
			Amount:         req.Amount,
			Status:         domain.TransferStatusPending,
		}
		if err := s.transferRepo.CreateTransfer(txCtx, transfer); err != nil {
			if postgres.IsDuplicateKeyError(err) {

				existingTransfer, fetchErr := s.transferRepo.GetTransferByIdempotencyKey(txCtx, req.IdempotencyKey)

				if fetchErr != nil {
					return fetchErr
				}

				createdTransfer = existingTransfer

				return nil
			}

			return fmt.Errorf("failed to create transfer: %w", err)
		}

		// Update wallet balances
		newFromBalance := fromWallet.Balance - req.Amount
		newToBalance := toWallet.Balance + req.Amount
		if err := s.walletRepo.UpdateWallet(txCtx, fromWallet.ID, newFromBalance); err != nil {
			return fmt.Errorf("failed to update from wallet balance: %w", err)
		}
		if err := s.walletRepo.UpdateWallet(txCtx, toWallet.ID, newToBalance); err != nil {
			return fmt.Errorf("failed to update to wallet balance: %w", err)
		}

		// Create ledger entries
		debitEntry := &domain.LedgerEntry{
			ID:         uuid.New(),
			TransferID: transfer.ID,
			WalletID:   fromWallet.ID,
			Amount:     req.Amount,
			EntryType:  domain.LedgerEntryTypeDebit,
		}
		creditEntry := &domain.LedgerEntry{
			ID:         uuid.New(),
			TransferID: transfer.ID,
			WalletID:   toWallet.ID,
			Amount:     req.Amount,
			EntryType:  domain.LedgerEntryTypeCredit,
		}
		if err := s.ledgerRepo.Create(txCtx, debitEntry); err != nil {
			return fmt.Errorf("failed to create debit ledger entry: %w", err)
		}
		if err := s.ledgerRepo.Create(txCtx, creditEntry); err != nil {
			return fmt.Errorf("failed to create credit ledger entry: %w", err)
		}

		// Update transfer status to processed
		if err := s.transferRepo.UpdateTransferStatus(txCtx, transfer.ID, domain.TransferStatusProcessed); err != nil {
			return fmt.Errorf("failed to update transfer status: %w", err)
		}
		transfer.Status = domain.TransferStatusProcessed
		createdTransfer = transfer
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to create transfer: %w", err)
	}

	return createdTransfer, nil
}
