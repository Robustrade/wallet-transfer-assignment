package service

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/RKShukla1997/wallet-transfer-assignment/internal/domain"
	"github.com/RKShukla1997/wallet-transfer-assignment/internal/repository"
)

// WalletService exposes read/seed operations for wallets. It intentionally
// contains no transfer workflow logic.
type WalletService struct {
	pool       *pgxpool.Pool
	walletRepo *repository.WalletRepository
}

func NewWalletService(pool *pgxpool.Pool, walletRepo *repository.WalletRepository) *WalletService {
	return &WalletService{pool: pool, walletRepo: walletRepo}
}

// GetWallet returns the current balance for a wallet.
func (s *WalletService) GetWallet(ctx context.Context, id string) (domain.Wallet, error) {
	var w domain.Wallet
	err := s.pool.AcquireFunc(ctx, func(conn *pgxpool.Conn) error {
		var err error
		w, err = s.walletRepo.Get(ctx, conn, id)
		return err
	})
	if err != nil {
		return domain.Wallet{}, fmt.Errorf("getting wallet: %w", err)
	}
	return w, nil
}

// CreateWallet seeds a new wallet with a starting balance. This is a
// convenience for demos/tests, not a core transfer requirement.
func (s *WalletService) CreateWallet(ctx context.Context, id string, startingBalance int64) (domain.Wallet, error) {
	if startingBalance < 0 {
		return domain.Wallet{}, domain.ErrInvalidAmount
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.Wallet{}, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	if err := s.walletRepo.Create(ctx, tx, id, startingBalance); err != nil {
		return domain.Wallet{}, err
	}
	w, err := s.walletRepo.Get(ctx, tx, id)
	if err != nil {
		return domain.Wallet{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Wallet{}, fmt.Errorf("committing transaction: %w", err)
	}
	return w, nil
}
