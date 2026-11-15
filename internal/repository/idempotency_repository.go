package repository

import (
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
)

type idempotencyRecordModel struct {
	Key         string    `gorm:"column:key;primaryKey"`
	RequestHash string    `gorm:"column:request_hash"`
	CreatedAt   time.Time `gorm:"column:created_at"`
}

func (idempotencyRecordModel) TableName() string {
	return "idempotency_records"
}

func (postgresRepository) ClaimIdempotencyKey(db *gorm.DB, key, requestHash string) (bool, error) {
	result := db.Clauses(clause.OnConflict{DoNothing: true}).
		Create(&idempotencyRecordModel{Key: key, RequestHash: requestHash})
	if result.Error != nil {
		if isLockContention(result.Error) {
			return false, domain.ErrServiceBusy
		}
		return false, fmt.Errorf("claim idempotency key: %w", result.Error)
	}
	return result.RowsAffected == 1, nil
}

func (postgresRepository) GetIdempotencyRequestHash(db *gorm.DB, key string) (string, error) {
	var model idempotencyRecordModel
	if err := db.First(&model, "key = ?", key).Error; err != nil {
		return "", fmt.Errorf("get idempotency record: %w", err)
	}
	return model.RequestHash, nil
}
