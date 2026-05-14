package service

import (
	"context"

	"wallet-transfer/internal/database"
	walletrepo "wallet-transfer/internal/wallet/repository"
	"wallet-transfer/pkg/errors"
	"wallet-transfer/pkg/logger"
)

// WalletService handles wallet business logic
type WalletService struct {
	db   *database.Connection
	repo *walletrepo.WalletRepository
	log  *logger.Logger
}

// NewWalletService creates a new wallet service
func NewWalletService(db *database.Connection, repo *walletrepo.WalletRepository, log *logger.Logger) *WalletService {
	return &WalletService{
		db:   db,
		repo: repo,
		log:  log,
	}
}

// GetBalance retrieves wallet balance
func (s *WalletService) GetBalance(ctx context.Context, walletID string) (int64, error) {
	tx, err := s.db.BeginTxReadOnly(ctx)
	if err != nil {
		return 0, errors.ErrInternalError
	}
	defer tx.Rollback()

	wallet, err := s.repo.GetByID(ctx, tx, walletID)
	if err != nil {
		s.log.Error("failed to get wallet", map[string]interface{}{
			"error": err.Error(),
		})
		return 0, errors.ErrDatabaseError
	}
	if wallet == nil {
		return 0, errors.ErrWalletNotFound
	}

	return wallet.Balance, nil
}

// GetWallet retrieves full wallet details
func (s *WalletService) GetWallet(ctx context.Context, walletID string) (*WalletDTO, error) {
	tx, err := s.db.BeginTxReadOnly(ctx)
	if err != nil {
		return nil, errors.ErrInternalError
	}
	defer tx.Rollback()

	wallet, err := s.repo.GetByID(ctx, tx, walletID)
	if err != nil {
		s.log.Error("failed to get wallet", map[string]interface{}{
			"error": err.Error(),
		})
		return nil, errors.ErrDatabaseError
	}
	if wallet == nil {
		return nil, errors.ErrWalletNotFound
	}

	return &WalletDTO{
		WalletID:  wallet.ID,
		Balance:   wallet.Balance,
		Currency:  wallet.Currency,
		CreatedAt: wallet.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt: wallet.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}, nil
}

// WalletDTO represents wallet details
type WalletDTO struct {
	WalletID  string `json:"walletId"`
	Balance   int64  `json:"balance"`
	Currency  string `json:"currency"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}
