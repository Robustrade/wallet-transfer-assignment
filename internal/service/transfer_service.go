package service

import (
	"context"
	"fmt"

	"github.com/skc1234/wallet-transfer-assignment/internal/domain"
	"github.com/skc1234/wallet-transfer-assignment/internal/repository"
)

type TransferService struct {
	Repo *repository.TransferRepository
}

func NewTransferService(
	repo *repository.TransferRepository,
) *TransferService {
	return &TransferService{
		Repo: repo,
	}
}

func (s *TransferService) TransferMoney(
	ctx context.Context,
	idempotencyKey string,
	fromWalletID string,
	toWalletID string,
	amount int64,
) (domain.Transfer, error) {

	if err := ValidateTransfer(
		fromWalletID,
		toWalletID,
		amount,
	); err != nil {
		return domain.Transfer{}, err
	}

	transfer, err := s.Repo.CreateTransfer(
		ctx,
		idempotencyKey,
		fromWalletID,
		toWalletID,
		amount,
	)
	if err != nil {
		return domain.Transfer{}, fmt.Errorf(
			"transfer failed: %w", err,
		)
	}

	return transfer, nil
}
