package postgres

import (
	"context"

	"github.com/Robustrade/wallet-transfer-assignment/internal/model"
	"github.com/Robustrade/wallet-transfer-assignment/internal/repository"
	"gorm.io/gorm"
)

type idempotencyRepository struct {
	db *gorm.DB
}

// NewIdempotencyRepository returns a Postgres-backed IdempotencyRepository.
func NewIdempotencyRepository(db *gorm.DB) repository.IdempotencyRepository {
	return &idempotencyRepository{db: db}
}

func (r *idempotencyRepository) GetByKey(ctx context.Context, tx *gorm.DB, key string) (*model.IdempotencyRecord, error) {
	if tx == nil {
		tx = r.db.WithContext(ctx)
	}

	var record model.IdempotencyRecord
	if err := tx.Where("idempotency_key = ?", key).First(&record).Error; err != nil {
		return nil, translateError(err)
	}
	return &record, nil
}

func (r *idempotencyRepository) Create(ctx context.Context, tx *gorm.DB, record model.IdempotencyRecord) error {
	if tx == nil {
		tx = r.db.WithContext(ctx)
	}

	return translateError(tx.Create(&record).Error)
}
