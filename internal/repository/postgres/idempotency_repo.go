package postgres

import (
	"context"
	"errors"
	"fmt"
	"wallet-transfer/internal/domain"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
)

type idempotencyRepository struct{}

func NewIdempotencyRepository() *idempotencyRepository {
	return &idempotencyRepository{}
}

func (r *idempotencyRepository) CreatePending(ctx context.Context, db *gorm.DB, key string) error {
	record := &domain.IdempotencyRecord{
		IdempotencyKey: key,
		Status:         domain.IdempotencyStatusPending,
	}
	err := db.WithContext(ctx).Create(record).Error
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" { // Unique violation in PG
			return domain.ErrDuplicateRequest
		}
		return fmt.Errorf("failed to create pending idempotency record: %w", err)
	}
	return nil
}

func (r *idempotencyRepository) Get(ctx context.Context, db *gorm.DB, key string) (*domain.IdempotencyRecord, error) {
	var rec domain.IdempotencyRecord
	err := db.WithContext(ctx).First(&rec, "idempotency_key = ?", key).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get idempotency record: %w", err)
	}
	return &rec, nil
}

func (r *idempotencyRepository) Update(ctx context.Context, db *gorm.DB, record *domain.IdempotencyRecord) error {
	err := db.WithContext(ctx).Save(record).Error
	if err != nil {
		return fmt.Errorf("failed to update idempotency record: %w", err)
	}
	return nil
}
