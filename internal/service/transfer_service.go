package service

import (
	"context"

	"wallet-service/internal/domain"
	"wallet-service/internal/repository"
)

type TransferService interface {
	Transfer(ctx context.Context, idempotencyKey, fromWalletID, toWalletID string, amount int64) (*domain.Transfer, error)
}

type transferService struct {
	repo repository.Repository
}

func NewTransferService(repo repository.Repository) TransferService {
	return &transferService{repo: repo}
}

func (s *transferService) Transfer(ctx context.Context, idempotencyKey, fromWalletID, toWalletID string, amount int64) (*domain.Transfer, error) {
	if fromWalletID == toWalletID {
		return nil, domain.ErrSameWallet
	}
	if amount <= 0 {
		return nil, domain.ErrInvalidAmount
	}

	// 1. Try to fetch existing transfer by idempotency key
	existingTransfer, err := s.repo.GetTransferByIdempotencyKey(ctx, idempotencyKey)
	if err != nil {
		return nil, err
	}
	if existingTransfer != nil {
		return existingTransfer, nil
	}

	req := &domain.Transfer{
		IdempotencyKey: idempotencyKey,
		FromWalletID:   fromWalletID,
		ToWalletID:     toWalletID,
		Amount:         amount,
		State:          domain.TransferStatePending,
	}

	// Executes transfer in a transaction.
	transfer, err := s.repo.ExecuteTransfer(ctx, req)
	if err != nil {
		if err == domain.ErrDuplicateTransfer {
			// Race condition: another request inserted it just now
			return s.repo.GetTransferByIdempotencyKey(ctx, idempotencyKey)
		}
		return nil, err
	}
	return transfer, nil
}
