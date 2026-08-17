package service

import (
	"context"
	"fmt"

	"github.com/raknay/wallet-transfer-assignment/internal/wallet/domain"
	"github.com/raknay/wallet-transfer-assignment/internal/wallet/repository"
)

type WalletService interface {
	CreateTransfer(ctx context.Context, request domain.Transfer) (domain.Transfer, error)
}

type walletService struct {
	repository repository.WalletRepository
}

func NewWalletService(repo repository.WalletRepository) WalletService {
	return &walletService{
		repository: repo,
	}
}

func (ts *walletService) CreateTransfer(ctx context.Context, request domain.Transfer) (domain.Transfer, error) {
	if err := request.Validate(); err != nil {
		return domain.Transfer{}, err
	}

	transfer, err := ts.repository.CreateTransfer(ctx, request)
	if err != nil {
		return domain.Transfer{}, fmt.Errorf("create transfer error: %w", err)
	}
	return transfer, nil

}
