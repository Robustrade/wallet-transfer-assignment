package service

import (
	"context"

	"github.com/robustrade/wallet-transfer-assignment/internal/model"
	"github.com/robustrade/wallet-transfer-assignment/internal/repository"
)

type TransferService struct {
	repo repository.WalletRepository
}

func NewTransferService(r repository.WalletRepository) *TransferService {
	return &TransferService{repo: r}
}

func (s *TransferService) Transfer(ctx context.Context, transfer *model.Transfer) error {

	// business logic
	return s.repo.Transfer(ctx, transfer)

}
