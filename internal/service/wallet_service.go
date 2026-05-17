package service

import (
	"context"
	"errors"
	"wallet-transfer-service/internal/domain"
	"wallet-transfer-service/internal/repository"
)

type WalletService struct {
	walletRepo *repository.WalletRepository
}

func NewWalletService(walletRepo *repository.WalletRepository) *WalletService {
	return &WalletService{
		walletRepo: walletRepo,
	}
}

func (s *WalletService) GetWallet(ctx context.Context, id string) (*domain.Wallet, error) {
	return s.walletRepo.GetWalletByID(ctx, id)
}

func (s *WalletService) CreateTransfer(fromWalletID, toWalletID, idempotencyKey string, amount int64) error {

	if amount <= 0 {
		return errors.New("amount must be greater than zero")
	}

	if fromWalletID == toWalletID {
		return errors.New("cannot transfer to same wallet")
	}

	if idempotencyKey == "" {
		return errors.New("idempotency key is required")
	}

	return nil
}
