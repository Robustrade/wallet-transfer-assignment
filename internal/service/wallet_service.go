package service

import (
	"context"
	"errors"
	"strings"

	"wallet-transfer-assignment/internal/domain"
	"wallet-transfer-assignment/internal/repository"
)

var (
	ErrInvalidWalletID       = errors.New("wallet id cannot be empty")
	ErrNegativeWalletBalance = errors.New("wallet balance cannot be negative")
)

type WalletService struct {
	repo repository.WalletRepository
}

func NewWalletService(repo repository.WalletRepository) *WalletService {
	return &WalletService{repo: repo}
}

func (s *WalletService) CreateWallet(ctx context.Context, id string, balance int64) (*domain.Wallet, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, ErrInvalidWalletID
	}
	if balance < 0 {
		return nil, ErrNegativeWalletBalance
	}
	return s.repo.CreateWallet(ctx, id, balance)
}

func (s *WalletService) GetWallet(ctx context.Context, id string) (*domain.Wallet, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, ErrInvalidWalletID
	}
	return s.repo.GetWallet(ctx, id)
}
