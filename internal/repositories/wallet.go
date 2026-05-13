package repositories

import (
	"context"

	"github.com/manish8561/wallet-transfer-assignment/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// WalletRepository handles wallet persistence
type WalletRepository struct {
	db *gorm.DB
}

// NewWalletRepository creates a new wallet repository
func NewWalletRepository(db *gorm.DB) *WalletRepository {
	return &WalletRepository{db: db}
}

// GetForUpdate retrieves a wallet for update with row-level lock
func (r *WalletRepository) GetForUpdate(ctx context.Context, tx *gorm.DB, walletID string) (*models.Wallet, error) {
	wallet := &models.Wallet{}
	err := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("wallet_id = ?", walletID).
		First(wallet).Error
	return wallet, err
}

// UpdateBalance updates wallet balance atomically
func (r *WalletRepository) UpdateBalance(ctx context.Context, tx *gorm.DB, walletID string, delta int64) error {
	return tx.WithContext(ctx).
		Model(&models.Wallet{}).
		Where("wallet_id = ?", walletID).
		Update("balance", gorm.Expr("balance + ?", delta)).Error
}

// GetBalance retrieves current wallet balance
func (r *WalletRepository) GetBalance(ctx context.Context, walletID string) (int64, error) {
	var balance int64
	err := r.db.WithContext(ctx).
		Model(&models.Wallet{}).
		Where("wallet_id = ?", walletID).
		Select("balance").
		Scan(&balance).Error
	return balance, err
}

// Create creates a new wallet
func (r *WalletRepository) Create(ctx context.Context, tx *gorm.DB, walletID string, initialBalance int64) (*models.Wallet, error) {
	wallet := &models.Wallet{
		WalletID: walletID,
		Balance:  initialBalance,
	}
	err := tx.WithContext(ctx).Create(wallet).Error
	return wallet, err
}

// GetByID retrieves a wallet by ID
func (r *WalletRepository) GetByID(ctx context.Context, walletID string) (*models.Wallet, error) {
	wallet := &models.Wallet{}
	err := r.db.WithContext(ctx).Where("wallet_id = ?", walletID).First(wallet).Error
	return wallet, err
}
