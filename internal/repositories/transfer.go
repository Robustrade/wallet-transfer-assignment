package repositories

import (
	"context"

	"github.com/manish8561/wallet-transfer-assignment/internal/models"
	"gorm.io/gorm"
)

// TransferRepository handles transfer persistence
type TransferRepository struct {
	db *gorm.DB
}

// NewTransferRepository creates a new transfer repository
func NewTransferRepository(db *gorm.DB) *TransferRepository {
	return &TransferRepository{db: db}
}

// Create creates a new transfer
func (r *TransferRepository) Create(ctx context.Context, tx *gorm.DB, fromWalletID, toWalletID string, amount int64, transferID string) (*models.Transfer, error) {
	transfer := &models.Transfer{
		TransferID:   transferID,
		FromWalletID: fromWalletID,
		ToWalletID:   toWalletID,
		Amount:       amount,
		Status:       models.TransferStatusPending,
	}
	err := tx.WithContext(ctx).Create(transfer).Error
	return transfer, err
}

// UpdateStatus updates transfer status
func (r *TransferRepository) UpdateStatus(ctx context.Context, tx *gorm.DB, transferID, newStatus string) error {
	return tx.WithContext(ctx).
		Model(&models.Transfer{}).
		Where("transfer_id = ?", transferID).
		Update("status", newStatus).Error
}

// GetByID retrieves a transfer by ID
func (r *TransferRepository) GetByID(ctx context.Context, transferID string) (*models.Transfer, error) {
	transfer := &models.Transfer{}
	err := r.db.WithContext(ctx).Where("transfer_id = ?", transferID).First(transfer).Error
	return transfer, err
}

// GetTransferHistory retrieves transfer history for a wallet
func (r *TransferRepository) GetTransferHistory(ctx context.Context, walletID string, limit, offset int) ([]*models.Transfer, error) {
	var transfers []*models.Transfer
	err := r.db.WithContext(ctx).
		Where("from_wallet_id = ? OR to_wallet_id = ?", walletID, walletID).
		Order("created_at DESC").
		Limit(limit).
		Offset(offset).
		Find(&transfers).Error
	return transfers, err
}
