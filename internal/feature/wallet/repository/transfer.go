package repository

import (
	"context"
	"errors"
	"fmt"

	"wallet-transfer-assignment/internal/feature/wallet/domain"
	"wallet-transfer-assignment/internal/feature/wallet/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type TransferRepository interface {
	Create(ctx context.Context, tx *gorm.DB, transfer *domain.Transfer) (bool, error)
	GetByIdempotencyKey(ctx context.Context, tx *gorm.DB, key string) (*domain.Transfer, error)
	UpdateStatus(ctx context.Context, tx *gorm.DB, transfer *domain.Transfer) error
}

type transferRepository struct{}

func NewTransferRepository() TransferRepository {
	return &transferRepository{}
}

// Create attempts to claim the idempotency key.
// Returns false when another transfer already owns the key.
func (r *transferRepository) Create(ctx context.Context, tx *gorm.DB, transfer *domain.Transfer) (bool, error) {
	record := model.Transfer{
		IdempotencyKey: transfer.IdempotencyKey,
		FromWalletID:   transfer.FromWalletID,
		ToWalletID:     transfer.ToWalletID,
		Amount:         transfer.Amount,
		Status:         string(domain.TransferPending),
	}

	result := tx.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "idempotency_key"}},
			DoNothing: true,
		}).
		Create(&record)

	if result.Error != nil {
		return false, fmt.Errorf("create transfer: %w", result.Error)
	}

	if result.RowsAffected == 0 {
		return false, nil
	}

	transfer.ID = record.ID
	transfer.Status = domain.TransferPending
	transfer.CreatedAt = record.CreatedAt
	transfer.UpdatedAt = record.UpdatedAt

	return true, nil
}

// GetByIdempotencyKey fetches an existing transfer.
func (r *transferRepository) GetByIdempotencyKey(ctx context.Context, tx *gorm.DB, key string) (*domain.Transfer, error) {
	var record model.Transfer

	err := tx.WithContext(ctx).
		Where("idempotency_key = ?", key).
		First(&record).Error

	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, gorm.ErrRecordNotFound
	}

	if err != nil {
		return nil, fmt.Errorf("get transfer: %w", err)
	}

	return toDomainTransfer(record), nil
}

// UpdateStatus persists a legal transfer status transition.
func (r *transferRepository) UpdateStatus(ctx context.Context, tx *gorm.DB, transfer *domain.Transfer) error {
	if transfer.Status != domain.TransferProcessed &&
		transfer.Status != domain.TransferFailed {
		return domain.ErrInvalidTransition
	}

	result := tx.WithContext(ctx).
		Model(&model.Transfer{}).
		Where("id = ? AND status = ?", transfer.ID, domain.TransferPending).
		Updates(map[string]interface{}{
			"status":         string(transfer.Status),
			"failure_reason": transfer.FailureReason,
			"updated_at":     gorm.Expr("NOW()"),
		})

	if result.Error != nil {
		return fmt.Errorf("update transfer status: %w", result.Error)
	}

	if result.RowsAffected != 1 {
		return domain.ErrInvalidTransition
	}

	return nil
}

func toDomainTransfer(record model.Transfer) *domain.Transfer {
	return &domain.Transfer{
		ID:             record.ID,
		IdempotencyKey: record.IdempotencyKey,
		FromWalletID:   record.FromWalletID,
		ToWalletID:     record.ToWalletID,
		Amount:         record.Amount,
		Status:         domain.TransferStatus(record.Status),
		FailureReason:  record.FailureReason,
		CreatedAt:      record.CreatedAt,
		UpdatedAt:      record.UpdatedAt,
	}
}
