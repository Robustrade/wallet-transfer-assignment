package service

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"wallet-transfer-assignment/internal/domain"
	"wallet-transfer-assignment/internal/repository"
)

type TransferService struct {
	txManager    repository.Transactor
	walletRepo   repository.WalletRepository
	transferRepo repository.TransferRepository
	ledgerRepo   repository.LedgerRepository
}

func NewTransferService(
	txManager repository.Transactor,
	walletRepo repository.WalletRepository,
	transferRepo repository.TransferRepository,
	ledgerRepo repository.LedgerRepository,
) *TransferService {
	return &TransferService{
		txManager:    txManager,
		walletRepo:   walletRepo,
		transferRepo: transferRepo,
		ledgerRepo:   ledgerRepo,
	}
}

func (s *TransferService) ExecuteTransfer(ctx context.Context, req domain.CreateTransferRequest) (*domain.Transfer, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	var createdTransfer *domain.Transfer

	err := s.txManager.WithinTransaction(ctx, func(txCtx context.Context) error {
		wallets, err := s.walletRepo.GetWalletsForUpdate(txCtx, req.FromWalletID, req.ToWalletID)
		if err != nil {
			return fmt.Errorf("failed to lock wallets: %w", err)
		}
		if len(wallets) < 2 {
			return domain.ErrWalletNotFound
		}

		var fromWallet, toWallet *domain.Wallet
		for i := range wallets {
			switch wallets[i].ID {
			case req.FromWalletID:
				fromWallet = &wallets[i]
			case req.ToWalletID:
				toWallet = &wallets[i]
			}
		}
		if fromWallet == nil || toWallet == nil {
			return domain.ErrWalletNotFound
		}

		// Insufficient funds: record transfer as FAILED without altering balances or ledger
		if fromWallet.Balance < req.Amount {
			reason := domain.ErrReasonInsufficientFunds
			failedTransfer := domain.Transfer{
				ID:             uuid.New().String(),
				IdempotencyKey: req.IdempotencyKey,
				FromWalletID:   req.FromWalletID,
				ToWalletID:     req.ToWalletID,
				Amount:         req.Amount,
				Status:         domain.StatusFailed,
				ErrorReason:    &reason,
			}
			createdTransfer, err = s.transferRepo.CreateTransfer(txCtx, failedTransfer)
			if err != nil {
				return fmt.Errorf("failed to record failed transfer: %w", err)
			}
			return nil
		}

		newFromBalance := fromWallet.Balance - req.Amount
		newToBalance := toWallet.Balance + req.Amount

		if err := s.walletRepo.UpdateWalletBalance(txCtx, fromWallet.ID, newFromBalance); err != nil {
			return fmt.Errorf("failed to update sender balance: %w", err)
		}

		if err := s.walletRepo.UpdateWalletBalance(txCtx, toWallet.ID, newToBalance); err != nil {
			return fmt.Errorf("failed to update receiver balance: %w", err)
		}

		transfer := domain.Transfer{
			ID:             uuid.New().String(),
			IdempotencyKey: req.IdempotencyKey,
			FromWalletID:   req.FromWalletID,
			ToWalletID:     req.ToWalletID,
			Amount:         req.Amount,
			Status:         domain.StatusProcessed,
		}
		createdTransfer, err = s.transferRepo.CreateTransfer(txCtx, transfer)
		if err != nil {
			return fmt.Errorf("failed to create transfer: %w", err)
		}

		debitEntry := domain.LedgerEntry{
			ID:         uuid.New().String(),
			TransferID: createdTransfer.ID,
			WalletID:   req.FromWalletID,
			Type:       domain.LedgerEntryDebit,
			Amount:     req.Amount,
		}
		if _, err := s.ledgerRepo.CreateLedgerEntry(txCtx, debitEntry); err != nil {
			return fmt.Errorf("failed to create debit ledger entry: %w", err)
		}

		creditEntry := domain.LedgerEntry{
			ID:         uuid.New().String(),
			TransferID: createdTransfer.ID,
			WalletID:   req.ToWalletID,
			Type:       domain.LedgerEntryCredit,
			Amount:     req.Amount,
		}
		if _, err := s.ledgerRepo.CreateLedgerEntry(txCtx, creditEntry); err != nil {
			return fmt.Errorf("failed to create credit ledger entry: %w", err)
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	if createdTransfer.Status == domain.StatusFailed {
		return nil, domain.ErrInsufficientFunds
	}

	return createdTransfer, nil
}
