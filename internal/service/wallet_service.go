package service

import (
	"context"
	"strings"

	"wallet-transfer/internal/domain"
)

type WalletRepository interface {
	CreateWallet(ctx context.Context, wallet *domain.Wallet) error
	GetWallet(ctx context.Context, walletID string) (*domain.Wallet, error)
	AddFunds(ctx context.Context, walletID string, amount int64) (*domain.Wallet, error)
	ListLedgerEntries(ctx context.Context, walletID string) ([]domain.LedgerEntry, error)
}

type WalletService interface {
	CreateWallet(ctx context.Context, input CreateWalletInput) (*domain.Wallet, error)
	GetWallet(ctx context.Context, walletID string) (*domain.Wallet, error)
	AddFunds(ctx context.Context, walletID string, amount int64) (*domain.Wallet, error)
	GetLedger(ctx context.Context, walletID string) ([]domain.LedgerEntry, error)
}

type WalletServiceImpl struct {
	repo WalletRepository
}

type CreateWalletInput struct {
	ID      string
	Balance int64
	Status  domain.WalletStatus
}

func NewWalletService(repo WalletRepository) WalletService {
	return &WalletServiceImpl{repo: repo}
}

func (s *WalletServiceImpl) CreateWallet(ctx context.Context, input CreateWalletInput) (*domain.Wallet, error) {
	if strings.TrimSpace(input.ID) == "" || input.Balance < 0 {
		return nil, domain.ErrInvalidWallet
	}
	status := input.Status
	if status == "" {
		status = domain.WalletStatusActive
	}
	if status != domain.WalletStatusActive && status != domain.WalletStatusInactive {
		return nil, domain.ErrInvalidWallet
	}
	wallet := &domain.Wallet{ID: strings.TrimSpace(input.ID), Balance: input.Balance, Status: status}
	if err := s.repo.CreateWallet(ctx, wallet); err != nil {
		return nil, err
	}
	return s.repo.GetWallet(ctx, wallet.ID)
}

func (s *WalletServiceImpl) GetWallet(ctx context.Context, walletID string) (*domain.Wallet, error) {
	if strings.TrimSpace(walletID) == "" {
		return nil, domain.ErrInvalidWallet
	}
	return s.repo.GetWallet(ctx, strings.TrimSpace(walletID))
}

func (s *WalletServiceImpl) AddFunds(ctx context.Context, walletID string, amount int64) (*domain.Wallet, error) {
	if strings.TrimSpace(walletID) == "" || amount <= 0 {
		return nil, domain.ErrInvalidWallet
	}
	return s.repo.AddFunds(ctx, strings.TrimSpace(walletID), amount)
}

func (s *WalletServiceImpl) GetLedger(ctx context.Context, walletID string) ([]domain.LedgerEntry, error) {
	if strings.TrimSpace(walletID) == "" {
		return nil, domain.ErrInvalidWallet
	}
	walletID = strings.TrimSpace(walletID)
	if _, err := s.repo.GetWallet(ctx, walletID); err != nil {
		return nil, err
	}
	return s.repo.ListLedgerEntries(ctx, walletID)
}
