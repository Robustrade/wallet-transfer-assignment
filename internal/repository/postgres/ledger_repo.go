package postgres

import (
	"context"
	"fmt"
	"wallet-transfer/internal/domain"

	"gorm.io/gorm"
)

type ledgerRepository struct{}

func NewLedgerRepository() *ledgerRepository {
	return &ledgerRepository{}
}

func (r *ledgerRepository) Create(ctx context.Context, db *gorm.DB, entry *domain.LedgerEntry) error {
	err := db.WithContext(ctx).Create(entry).Error
	if err != nil {
		return fmt.Errorf("failed to create ledger entry: %w", err)
	}
	return nil
}

func (r *ledgerRepository) GetByTransferID(ctx context.Context, db *gorm.DB, transferID string) ([]domain.LedgerEntry, error) {
	var entries []domain.LedgerEntry
	err := db.WithContext(ctx).Where("transfer_id = ?", transferID).Find(&entries).Error
	if err != nil {
		return nil, fmt.Errorf("failed to find ledger entries: %w", err)
	}
	return entries, nil
}
