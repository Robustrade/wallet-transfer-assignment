package service

import (
	"context"

	"wallet-transfer-assignment/internal/domain"
	"wallet-transfer-assignment/internal/repository/postgres"

	"github.com/google/uuid"
)

type WalletService struct {
	walletRepo *postgres.WalletRepository
	ledgerRepo *postgres.LedgerRepository
}

func NewWalletService(walletRepo *postgres.WalletRepository, ledgerRepo *postgres.LedgerRepository) *WalletService {
	return &WalletService{
		walletRepo: walletRepo,
		ledgerRepo: ledgerRepo,
	}
}

func (s *WalletService) CreateWallet(ctx context.Context, initialBalance domain.Money, currency string) (*domain.Wallet, error) {
	id := uuid.New()
	wallet, err := domain.NewWallet(id, initialBalance, currency)
	if err != nil {
		return nil, err
	}

	if err := s.walletRepo.CreateWallet(ctx, wallet); err != nil {
		return nil, err
	}

	return wallet, nil
}

func (s *WalletService) GetWalletBalance(ctx context.Context, walletID uuid.UUID) (*domain.Wallet, error) {
	return s.walletRepo.GetWalletByID(ctx, walletID)
}

func (s *WalletService) GetWalletTransactions(ctx context.Context, walletID uuid.UUID) ([]*domain.LedgerEntry, error) {
	// First check if wallet exists
	if _, err := s.walletRepo.GetWalletByID(ctx, walletID); err != nil {
		return nil, err
	}
	return s.ledgerRepo.GetLedgerEntriesByWalletID(ctx, walletID)
}
