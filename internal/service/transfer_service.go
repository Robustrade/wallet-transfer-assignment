package service

import (
	"context"

	"wallet-transfer-assignment/internal/domain"
	"wallet-transfer-assignment/internal/repository"
)

type CreateTransferRequest struct {
	FromWalletId   string `json:"fromWalletId"`
	ToWalletId     string `json:"toWalletId"`
	Amount         int64  `json:"amount"`
	IdempotencyKey string `json:"idempotencyKey"`
}
type TransferRepository interface {
	BeginTx(ctx context.Context) (*repository.TransferTx, error)
}

type TransferService struct {
	TransferRepository TransferRepository
}

func NewTransferService(TransferRepository TransferRepository) *TransferService {
	return &TransferService{TransferRepository: TransferRepository}
}

func (t *TransferService) CreateTransfer(ctx context.Context, req CreateTransferRequest) (*domain.Transfer, bool, error) {
	transfer := &domain.Transfer{
		FromWalletID: domain.WalletID(req.FromWalletId),
		ToWalletID:   domain.WalletID(req.ToWalletId),
		Status:       domain.TransferStatusPending,
		Amount:       req.Amount,
	}

	if err := transfer.Validate(); err != nil {
		return nil, false, err
	}

	tx, err := t.TransferRepository.BeginTx(ctx)
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback(ctx)

	existingTransfer, err := tx.GetTransferByIdempotencyKey(ctx, req.IdempotencyKey)
	if err != nil {
		return nil, false, err
	}
	if existingTransfer != nil {
		return existingTransfer, true, nil
	}

	fromWallet, toWallet, err := tx.LockWallets(ctx, transfer.FromWalletID, transfer.ToWalletID)
	if err != nil {
		return nil, false, err
	}

	if err := fromWallet.Debit(transfer.Amount); err != nil {
		return nil, false, err
	}

	if err := toWallet.Credit(transfer.Amount); err != nil {
		return nil, false, err
	}

	createdTransfer, err := tx.CreateTransfer(ctx, transfer)
	if err != nil {
		return nil, false, err
	}

	if err := tx.UpdateWalletBalance(ctx, fromWallet.ID, fromWallet.Balance); err != nil {
		return nil, false, err
	}

	if err := tx.UpdateWalletBalance(ctx, toWallet.ID, toWallet.Balance); err != nil {
		return nil, false, err
	}

	debitEntry, err := domain.NewDebitEntry(fromWallet.ID, createdTransfer.ID, createdTransfer.Amount)
	if err != nil {
		return nil, false, err
	}

	creditEntry, err := domain.NewCreditEntry(toWallet.ID, createdTransfer.ID, createdTransfer.Amount)
	if err != nil {
		return nil, false, err
	}

	if err := tx.CreateLedgerEntry(ctx, debitEntry); err != nil {
		return nil, false, err
	}

	if err := tx.CreateLedgerEntry(ctx, creditEntry); err != nil {
		return nil, false, err
	}

	if err := createdTransfer.MarkProcessed(); err != nil {
		return nil, false, err
	}

	if err := tx.UpdateTransferStatus(ctx, createdTransfer.ID, createdTransfer.Status); err != nil {
		return nil, false, err
	}

	if err := tx.SaveIdempotencyRecord(ctx, req.IdempotencyKey, createdTransfer.ID); err != nil {
		return nil, false, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, false, err
	}

	return createdTransfer, false, nil

}
