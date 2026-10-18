package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"wallet-transfer-assignment/internal/feature/wallet/domain"
	"wallet-transfer-assignment/internal/feature/wallet/model"

	"gorm.io/gorm"
)

// WalletRepository defines wallet database operations.
type WalletRepository interface {
	GetByID(ctx context.Context, tx *gorm.DB, id int64) (*domain.Wallet, error)
	GetByIDForUpdate(ctx context.Context, tx *gorm.DB, id int64) (*domain.Wallet, error)
	UpdateBalance(ctx context.Context, tx *gorm.DB, id int64, balance int64) error
}

type walletRepository struct{}

// NewWalletRepository creates a wallet
func NewWalletRepository() WalletRepository {
	return &walletRepository{}
}

// GetByID fetches a wallet without locking.
func (r *walletRepository) GetByID(ctx context.Context, tx *gorm.DB, id int64) (*domain.Wallet, error) {
	var wallet model.Wallet

	err := tx.WithContext(ctx).Where("id = ?", id).First(&wallet).Error

	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, domain.ErrWalletNotFound
	}

	if err != nil {
		return nil, fmt.Errorf("get wallet: %w", err)
	}

	return toDomainWallet(wallet), nil
}

// GetByIDForUpdate locks the wallet row until transaction commit/rollback.
func (r *walletRepository) GetByIDForUpdate(ctx context.Context, tx *gorm.DB, id int64) (*domain.Wallet, error) {
	var wallet model.Wallet

	if err := tx.WithContext(ctx).
		Raw(`SELECT id, balance FROM wallets WHERE id = ? FOR UPDATE`, id).
		Scan(&wallet).Error; err != nil {
		return nil, fmt.Errorf("lock wallet: %w", err)
	}

	if wallet.ID == 0 {
		return nil, domain.ErrWalletNotFound
	}
	return toDomainWallet(wallet), nil
}

// UpdateBalance atomically debits or credits a wallet.
func (r *walletRepository) UpdateBalance(ctx context.Context, tx *gorm.DB, id, amount int64) error {
	result := tx.WithContext(ctx).
		Model(&model.Wallet{}).Where("id = ?", id).
		Updates(map[string]interface{}{
			"balance":    gorm.Expr("balance + ?", amount),
			"updated_at": time.Now().UTC(),
		})

	if result.Error != nil {
		return fmt.Errorf("update wallet balance: %w", result.Error)
	}

	if result.RowsAffected != 1 {
		return fmt.Errorf("wallet not found or insufficient balance")
	}

	return nil
}

// toDomainWallet converts database model into domain entity.
func toDomainWallet(wallet model.Wallet) *domain.Wallet {
	return &domain.Wallet{
		ID:      wallet.ID,
		Balance: wallet.Balance,
	}
}
