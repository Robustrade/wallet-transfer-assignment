package repositories

import (
	"context"
	"encoding/json"

	"github.com/manish8561/wallet-transfer-assignment/internal/models"
	"gorm.io/gorm"
)

// IdempotencyRepository handles idempotency record persistence
type IdempotencyRepository struct {
	db *gorm.DB
}

// NewIdempotencyRepository creates a new idempotency repository
func NewIdempotencyRepository(db *gorm.DB) *IdempotencyRepository {
	return &IdempotencyRepository{db: db}
}

// GetByKey retrieves an idempotency record by key
func (r *IdempotencyRepository) GetByKey(ctx context.Context, idempotencyKey string) (*models.IdempotencyRecord, error) {
	record := &models.IdempotencyRecord{}
	err := r.db.WithContext(ctx).Where("idempotency_key = ?", idempotencyKey).First(record).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil // Not found is not an error
		}
		return nil, err
	}
	return record, nil
}

// Record stores an idempotency record
func (r *IdempotencyRepository) Record(ctx context.Context, tx *gorm.DB, idempotencyKey, transferID string, response interface{}) error {
	responseJSON, err := json.Marshal(response)
	if err != nil {
		return err
	}

	record := &models.IdempotencyRecord{
		IdempotencyKey: idempotencyKey,
		TransferID:     transferID,
		Response:       responseJSON,
	}

	err = tx.WithContext(ctx).Create(record).Error
	if err != nil {
		// Handle unique constraint violation (already recorded)
		// GORM will return a specific error for this
		return nil // Silently ignore duplicate, it's idempotent
	}
	return err
}
