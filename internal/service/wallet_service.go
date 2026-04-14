package service

import (
	"context"
	"errors"

	"github.com/Robustrade/wallet-transfer-assignment/internal/model"
	"github.com/Robustrade/wallet-transfer-assignment/internal/repository"
)

type WalletService interface {
	GetWalletBalance(ctx context.Context, walletID string) (*model.Wallet, error)
	GetWalletTransfers(ctx context.Context, walletID string) ([]*model.Transfer, error)
}

type walletService struct {
	repo repository.Repository
}

func NewWalletService(repo repository.Repository) WalletService {
	return &walletService{repo: repo}
}

func (s *walletService) GetWalletBalance(ctx context.Context, walletID string) (*model.Wallet, error) {
	wallet, err := s.repo.GetWallet(ctx, walletID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrWalletNotFound
		}
		return nil, err
	}
	return wallet, nil
}

func (s *walletService) GetWalletTransfers(ctx context.Context, walletID string) ([]*model.Transfer, error) {
	_, err := s.GetWalletBalance(ctx, walletID) // Verify wallet exists natively
	if err != nil {
		return nil, err // Returns layer-decoupled ErrWalletNotFound natively
	}
	return s.repo.GetTransfersByWalletID(ctx, walletID)
}
