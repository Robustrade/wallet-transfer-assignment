package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/Robustrade/wallet-transfer-assignment/internal/model"
	"github.com/Robustrade/wallet-transfer-assignment/internal/repository"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type walletRepository struct {
	db *gorm.DB
}

// NewWalletRepository returns a Postgres-backed WalletRepository.
func NewWalletRepository(db *gorm.DB) repository.WalletRepository {
	return &walletRepository{db: db}
}

func (r *walletRepository) GetWalletForUpdate(ctx context.Context, tx *gorm.DB, walletID string) (*model.Wallet, error) {
	if tx == nil {
		tx = r.db.WithContext(ctx)
	}

	var wallet model.Wallet
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("wallet_id = ?", walletID).First(&wallet).Error; err != nil {
		return nil, translateError(err)
	}
	return &wallet, nil
}

func (r *walletRepository) UpdateBalance(ctx context.Context, tx *gorm.DB, walletID string, newBalance int64) error {
	if tx == nil {
		tx = r.db.WithContext(ctx)
	}

	result := tx.Model(&model.Wallet{}).
		Where("wallet_id = ?", walletID).
		Updates(map[string]any{"balance": newBalance, "updated_at": time.Now().UTC()})
	if result.Error != nil {
		return translateError(result.Error)
	}
	if result.RowsAffected != 1 {
		return repository.ErrNotFound
	}
	return nil
}

// translateError maps GORM/driver errors to repository-level sentinels so that
// callers depend only on the repository abstraction, not on the ORM.
func translateError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return repository.ErrNotFound
	}
	if repository.IsUniqueViolation(err) {
		return repository.ErrUniqueViolation
	}
	return err
}
