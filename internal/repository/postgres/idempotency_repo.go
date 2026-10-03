package postgres

import (
	"context"
	"errors"

	"wallet-transfer-assignment/internal/domain"
	"wallet-transfer-assignment/internal/repository"

	"gorm.io/gorm"
)

type idempotencyRepo struct {
	db *gorm.DB
}

// NewIdempotencyRepository creates a new GORM-backed IdempotencyRepository.
func NewIdempotencyRepository(db *gorm.DB) repository.IdempotencyRepository {
	return &idempotencyRepo{db: db}
}

func (r *idempotencyRepo) Get(ctx context.Context, key string) (*domain.IdempotencyRecord, error) {
	var record domain.IdempotencyRecord
	err := r.db.WithContext(ctx).First(&record, "key = ?", key).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil // Return nil, nil when record does not exist
		}
		return nil, err
	}
	return &record, nil
}

func (r *idempotencyRepo) Create(ctx context.Context, tx *gorm.DB, record *domain.IdempotencyRecord) error {
	dbConn := r.db
	if tx != nil {
		dbConn = tx
	}
	return dbConn.WithContext(ctx).Create(record).Error
}

func (r *idempotencyRepo) Update(ctx context.Context, tx *gorm.DB, record *domain.IdempotencyRecord) error {
	dbConn := r.db
	if tx != nil {
		dbConn = tx
	}
	return dbConn.WithContext(ctx).Save(record).Error
}
