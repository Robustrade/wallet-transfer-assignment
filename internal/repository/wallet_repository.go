package repository

import (
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
)

type walletModel struct {
	ID        string    `gorm:"column:id;primaryKey"`
	Balance   int64     `gorm:"column:balance"`
	CreatedAt time.Time `gorm:"column:created_at"`
	UpdatedAt time.Time `gorm:"column:updated_at"`
}

func (walletModel) TableName() string {
	return "wallets"
}

func (m walletModel) toDomain() *domain.Wallet {
	return &domain.Wallet{
		ID:        m.ID,
		Balance:   m.Balance,
		CreatedAt: m.CreatedAt,
		UpdatedAt: m.UpdatedAt,
	}
}

func (postgresRepository) CreateWallet(db *gorm.DB, wallet *domain.Wallet) error {
	model := walletModel{ID: wallet.ID, Balance: wallet.Balance}
	if err := db.Create(&model).Error; err != nil {
		if isUniqueViolation(err) {
			return domain.ErrWalletAlreadyExists
		}
		return fmt.Errorf("insert wallet: %w", err)
	}
	wallet.CreatedAt = model.CreatedAt
	wallet.UpdatedAt = model.UpdatedAt
	return nil
}

func (postgresRepository) LockWallets(db *gorm.DB, ids []string) ([]*domain.Wallet, error) {
	var models []walletModel
	err := db.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id IN ?", ids).
		Order("id").
		Find(&models).Error
	if err != nil {
		if isLockContention(err) {
			return nil, domain.ErrServiceBusy
		}
		return nil, fmt.Errorf("lock wallets: %w", err)
	}

	wallets := make([]*domain.Wallet, len(models))
	for i, m := range models {
		wallets[i] = m.toDomain()
	}
	return wallets, nil
}

func (postgresRepository) UpdateWalletBalance(db *gorm.DB, wallet *domain.Wallet) error {
	now := time.Now()
	result := db.Model(&walletModel{}).
		Where("id = ?", wallet.ID).
		Updates(map[string]any{"balance": wallet.Balance, "updated_at": now})
	if result.Error != nil {
		return fmt.Errorf("update wallet balance: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("update wallet balance: wallet %q not found", wallet.ID)
	}
	wallet.UpdatedAt = now
	return nil
}
