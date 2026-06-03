package postgres

import (
	"context"
	"errors"
	"fmt"
	"wallet-transfer/internal/domain"

	"gorm.io/gorm"
)

type transferRepository struct{}

func NewTransferRepository() *transferRepository {
	return &transferRepository{}
}

func (r *transferRepository) Create(ctx context.Context, db *gorm.DB, transfer *domain.Transfer) error {
	err := db.WithContext(ctx).Create(transfer).Error
	if err != nil {
		return fmt.Errorf("failed to create transfer: %w", err)
	}
	return nil
}

func (r *transferRepository) Get(ctx context.Context, db *gorm.DB, id string) (*domain.Transfer, error) {
	var t domain.Transfer
	err := db.WithContext(ctx).First(&t, "id = ?", id).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("transfer not found")
		}
		return nil, fmt.Errorf("failed to get transfer: %w", err)
	}
	return &t, nil
}
