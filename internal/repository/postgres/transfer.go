package postgres

import (
	"context"
	"time"

	"github.com/Robustrade/wallet-transfer-assignment/internal/model"
	"github.com/Robustrade/wallet-transfer-assignment/internal/repository"
	"gorm.io/gorm"
)

type transferRepository struct {
	db *gorm.DB
}

// NewTransferRepository returns a Postgres-backed TransferRepository.
func NewTransferRepository(db *gorm.DB) repository.TransferRepository {
	return &transferRepository{db: db}
}

func (r *transferRepository) CreateTransfer(ctx context.Context, tx *gorm.DB, transfer model.Transfer) error {
	if tx == nil {
		tx = r.db.WithContext(ctx)
	}
	return translateError(tx.Create(&transfer).Error)
}

func (r *transferRepository) GetByID(ctx context.Context, tx *gorm.DB, transferID string) (*model.Transfer, error) {
	if tx == nil {
		tx = r.db.WithContext(ctx)
	}

	var transfer model.Transfer
	if err := tx.Where("transfer_id = ?", transferID).First(&transfer).Error; err != nil {
		return nil, translateError(err)
	}
	return &transfer, nil
}

func (r *transferRepository) UpdateTransferState(ctx context.Context, tx *gorm.DB, transferID string, state model.TransferState) error {
	if tx == nil {
		tx = r.db.WithContext(ctx)
	}

	result := tx.Model(&model.Transfer{}).
		Where("transfer_id = ?", transferID).
		Updates(map[string]any{"state": state, "updated_at": time.Now().UTC()})
	if result.Error != nil {
		return translateError(result.Error)
	}
	if result.RowsAffected != 1 {
		return repository.ErrNotFound
	}
	return nil
}
