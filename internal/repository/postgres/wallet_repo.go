package postgres

import (
	"context"
	"errors"
	"fmt"
	"wallet-transfer/internal/domain"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type walletRepository struct{}

func NewWalletRepository() *walletRepository {
	return &walletRepository{}
}

func (r *walletRepository) Get(ctx context.Context, db *gorm.DB, id string) (*domain.Wallet, error) {
	var wallet domain.Wallet
	err := db.WithContext(ctx).First(&wallet, "id = ?", id).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrWalletNotFound
		}
		return nil, fmt.Errorf("failed to get wallet: %w", err)
	}
	return &wallet, nil
}

func (r *walletRepository) GetWithLock(ctx context.Context, db *gorm.DB, id string) (*domain.Wallet, error) {
	var wallet domain.Wallet
	err := db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).First(&wallet, "id = ?", id).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrWalletNotFound
		}
		return nil, fmt.Errorf("failed to get wallet with lock: %w", err)
	}
	return &wallet, nil
}

func (r *walletRepository) UpdateBalance(ctx context.Context, db *gorm.DB, id string, amount int64) error {
	res := db.WithContext(ctx).Model(&domain.Wallet{}).Where("id = ?", id).Update("balance", gorm.Expr("balance + ?", amount))
	if res.Error != nil {
		return fmt.Errorf("failed to update wallet balance: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return domain.ErrWalletNotFound
	}
	return nil
}

func (r *walletRepository) Create(ctx context.Context, db *gorm.DB, wallet *domain.Wallet) error {
	err := db.WithContext(ctx).Create(wallet).Error
	if err != nil {
		return fmt.Errorf("failed to create wallet: %w", err)
	}
	return nil
}
