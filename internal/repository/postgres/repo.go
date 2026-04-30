package postgres

import (
	"context"
	"errors"
	"strings"

	"github.com/gerry1818/wallet-transfer-assignment/internal/model"
	"github.com/gerry1818/wallet-transfer-assignment/internal/repository"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
)

type Repo struct {
	db *gorm.DB
}

func NewRepo(db *gorm.DB) *Repo {
	return &Repo{db: db}
}

func (r *Repo) ClaimIdempotency(ctx context.Context, key, hash string) (bool, error) {
	// Try to create a new idempotency record
	result := r.db.WithContext(ctx).Create(&model.IdempotencyRecord{
		IdempotencyKey:  key,
		RequestHash:     hash,
		Status:          "IN_PROGRESS",
		StatusCode:      0,
		ResponsePayload: "",
	})

	// First time insert → proceed
	if result.Error == nil && result.RowsAffected == 1 {
		return true, nil
	}

	// If unique constraint error, validate existing record
	if result.Error != nil {
		// Check if it's a duplicate key error (SQLite / GORM / PostgreSQL)
		var pgErr *pgconn.PgError
		isPGUniqueViolation := errors.As(result.Error, &pgErr) && pgErr.Code == "23505"
		isSQLiteUniqueViolation := strings.Contains(result.Error.Error(), "UNIQUE constraint failed")
		if isPGUniqueViolation || isSQLiteUniqueViolation || result.Error == gorm.ErrDuplicatedKey {
			// Record already exists, validate it
			var existing model.IdempotencyRecord
			err := r.db.WithContext(ctx).
				Where("idempotency_key = ?", key).
				First(&existing).Error

			if err != nil {
				return false, err
			}

			// Same request → allow reuse
			if existing.RequestHash == hash {
				return false, nil
			}

			// Different request hash for same key -> explicit hash mismatch
			return false, repository.ErrIdempotencyHashMismatch
		}
		// Other error
		return false, result.Error
	}

	return false, nil
}

func (r *Repo) GetIdempotency(ctx context.Context, key string) (string, int, error) {
	var record model.IdempotencyRecord

	err := r.db.WithContext(ctx).
		Where("idempotency_key = ?", key).
		First(&record).Error

	if err != nil {
		return "", 0, err
	}

	return record.ResponsePayload, record.StatusCode, nil
}

func (r *Repo) BeginTx(ctx context.Context) (repository.Tx, error) {
	tx := r.db.WithContext(ctx).Begin()
	if tx.Error != nil {
		return nil, tx.Error
	}

	return &txImpl{
		tx: tx,
	}, nil
}

func (r *Repo) UpdateIdempotency(ctx context.Context, key, status, resp string, code int) error {
	return r.db.WithContext(ctx).
		Model(&model.IdempotencyRecord{}).
		Where("idempotency_key = ?", key).
		Updates(map[string]interface{}{
			"status":           status,
			"status_code":      code,
			"response_payload": resp,
		}).Error
}
