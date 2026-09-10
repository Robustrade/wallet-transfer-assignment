package service

import (
	"context"
	"log/slog"
	"time"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
	"github.com/Robustrade/wallet-transfer-assignment/internal/repository"
	"github.com/jackc/pgx/v5"
)

// WalletService handles wallet-related business logic.
type WalletService struct {
	txManager  repository.TxManager
	walletRepo repository.WalletRepository
	ledgerRepo repository.LedgerRepository
	logger     *slog.Logger
}

// NewWalletService creates a new WalletService.
func NewWalletService(
	txManager repository.TxManager,
	walletRepo repository.WalletRepository,
	ledgerRepo repository.LedgerRepository,
	logger *slog.Logger,
) *WalletService {
	return &WalletService{
		txManager:  txManager,
		walletRepo: walletRepo,
		ledgerRepo: ledgerRepo,
		logger:     logger,
	}
}

// GetWallet retrieves a wallet by its ID.
func (s *WalletService) GetWallet(ctx context.Context, walletID string) (*domain.Wallet, error) {
	var wallet *domain.Wallet

	err := s.txManager.WithTx(ctx, func(tx pgx.Tx) error {
		var txErr error
		wallet, txErr = s.walletRepo.GetByID(ctx, tx, walletID)
		return txErr
	})

	return wallet, err
}

// CreateWallet creates a new wallet with the given ID and initial balance.
func (s *WalletService) CreateWallet(ctx context.Context, id string, initialBalance int64, currency string) (*domain.Wallet, error) {
	if id == "" {
		return nil, domain.ErrEmptyWalletID
	}
	if initialBalance < 0 {
		return nil, domain.ErrInvalidAmount
	}
	if currency == "" {
		currency = "USD"
	}

	wallet := &domain.Wallet{
		ID:        id,
		Balance:   initialBalance,
		Currency:  currency,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	err := s.txManager.WithTx(ctx, func(tx pgx.Tx) error {
		return s.walletRepo.Create(ctx, tx, wallet)
	})
	if err != nil {
		return nil, err
	}

	s.logger.InfoContext(ctx, "wallet created",
		slog.String("wallet_id", id),
		slog.Int64("initial_balance", initialBalance),
	)

	return wallet, nil
}

// GetTransactionHistory retrieves ledger entries for a wallet.
func (s *WalletService) GetTransactionHistory(ctx context.Context, walletID string, limit, offset int) ([]*domain.LedgerEntry, error) {
	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	var entries []*domain.LedgerEntry

	err := s.txManager.WithTx(ctx, func(tx pgx.Tx) error {
		// Verify wallet exists
		_, walletErr := s.walletRepo.GetByID(ctx, tx, walletID)
		if walletErr != nil {
			return walletErr
		}

		var txErr error
		entries, txErr = s.ledgerRepo.GetByWalletID(ctx, tx, walletID, limit, offset)
		return txErr
	})

	return entries, err
}
