package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"wallet-transfer-assignment/internal/feature/wallet/domain"
	"wallet-transfer-assignment/internal/feature/wallet/model"

	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type IdempotencyRepository interface {
	Create(ctx context.Context, tx *gorm.DB, record *domain.IdempotencyRecord) (bool, error)
	GetByKey(ctx context.Context, tx *gorm.DB, key string) (*domain.IdempotencyRecord, error)
	SaveResponse(ctx context.Context, tx *gorm.DB, record *domain.IdempotencyRecord) error
}

type idempotencyRepository struct{}

func NewIdempotencyRepository() IdempotencyRepository {
	return &idempotencyRepository{}
}

// Create attempts to insert an idempotency record.
// Returns false when the key already exists.
func (r *idempotencyRepository) Create(ctx context.Context, tx *gorm.DB, record *domain.IdempotencyRecord) (bool, error) {
	dbRecord := model.IdempotencyRecord{
		IdempotencyKey: record.IdempotencyKey,
		RequestHash:    record.RequestHash,
		TransferID:     record.TransferID,
	}

	result := tx.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "idempotency_key"}},
			DoNothing: true,
		}).
		Create(&dbRecord)

	if result.Error != nil {
		return false, fmt.Errorf("create idempotency record: %w", result.Error)
	}

	if result.RowsAffected == 0 {
		return false, nil
	}

	record.ID = dbRecord.ID
	record.CreatedAt = dbRecord.CreatedAt
	record.UpdatedAt = dbRecord.UpdatedAt

	return true, nil
}

// GetByKey fetches an existing idempotency record.
func (r *idempotencyRepository) GetByKey(ctx context.Context, tx *gorm.DB, key string) (*domain.IdempotencyRecord, error) {
	var dbRecord model.IdempotencyRecord

	err := tx.WithContext(ctx).
		Where("idempotency_key = ?", key).
		First(&dbRecord).Error

	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, domain.ErrIdempotencyNotFound
	}

	if err != nil {
		return nil, fmt.Errorf("get idempotency record: %w", err)
	}

	return toDomainIdempotency(dbRecord), nil
}

// SaveResponse stores the original HTTP response for future retries.
func (r *idempotencyRepository) SaveResponse(ctx context.Context, tx *gorm.DB, record *domain.IdempotencyRecord) error {
	if record.ResponseStatus == nil ||
		len(record.ResponseBody) == 0 {
		return domain.ErrInvalidIdempotency
	}

	result := tx.WithContext(ctx).
		Model(&model.IdempotencyRecord{}).
		Where("id = ? AND response_status IS NULL", record.ID).
		Updates(map[string]interface{}{
			"transfer_id":     record.TransferID,
			"response_status": *record.ResponseStatus,
			"response_body":   datatypes.JSON(record.ResponseBody),
			"updated_at":      gorm.Expr("NOW()"),
		})

	if result.Error != nil {
		return fmt.Errorf("save idempotency response: %w", result.Error)
	}

	if result.RowsAffected != 1 {
		return domain.ErrIdempotencyConflict
	}

	return nil
}

// Convert database model to domain entity.
func toDomainIdempotency(record model.IdempotencyRecord) *domain.IdempotencyRecord {
	return &domain.IdempotencyRecord{
		ID:             record.ID,
		IdempotencyKey: record.IdempotencyKey,
		RequestHash:    record.RequestHash,
		TransferID:     record.TransferID,
		ResponseStatus: record.ResponseStatus,
		ResponseBody:   json.RawMessage(record.ResponseBody),
		CreatedAt:      record.CreatedAt,
		UpdatedAt:      record.UpdatedAt,
	}
}
