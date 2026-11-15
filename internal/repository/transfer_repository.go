package repository

import (
	"fmt"
	"time"

	"gorm.io/gorm"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
)

type transferModel struct {
	ID             string    `gorm:"column:id;primaryKey"`
	IdempotencyKey string    `gorm:"column:idempotency_key"`
	FromWalletID   string    `gorm:"column:from_wallet_id"`
	ToWalletID     string    `gorm:"column:to_wallet_id"`
	Amount         int64     `gorm:"column:amount"`
	Status         string    `gorm:"column:status"`
	FailureReason  *string   `gorm:"column:failure_reason"`
	CreatedAt      time.Time `gorm:"column:created_at"`
	UpdatedAt      time.Time `gorm:"column:updated_at"`
}

func (transferModel) TableName() string {
	return "transfers"
}

func (m transferModel) toDomain() *domain.Transfer {
	transfer := &domain.Transfer{
		ID:             m.ID,
		IdempotencyKey: m.IdempotencyKey,
		FromWalletID:   m.FromWalletID,
		ToWalletID:     m.ToWalletID,
		Amount:         m.Amount,
		Status:         domain.TransferStatus(m.Status),
		CreatedAt:      m.CreatedAt.UTC(),
		UpdatedAt:      m.UpdatedAt.UTC(),
	}
	if m.FailureReason != nil {
		transfer.FailureReason = domain.FailureReason(*m.FailureReason)
	}
	return transfer
}

func (postgresRepository) CreateTransfer(db *gorm.DB, transfer *domain.Transfer) error {
	now := dbNow()
	model := transferModel{
		ID:             transfer.ID,
		IdempotencyKey: transfer.IdempotencyKey,
		FromWalletID:   transfer.FromWalletID,
		ToWalletID:     transfer.ToWalletID,
		Amount:         transfer.Amount,
		Status:         string(transfer.Status),
		FailureReason:  nullableReason(transfer.FailureReason),
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := db.Create(&model).Error; err != nil {
		return fmt.Errorf("insert transfer: %w", err)
	}
	transfer.CreatedAt = now
	transfer.UpdatedAt = now
	return nil
}

func (postgresRepository) UpdateTransferStatus(db *gorm.DB, transfer *domain.Transfer) error {
	now := dbNow()
	result := db.Model(&transferModel{}).
		Where("id = ? AND status = ?", transfer.ID, string(domain.StatusPending)).
		Updates(map[string]any{
			"status":         string(transfer.Status),
			"failure_reason": nullableReason(transfer.FailureReason),
			"updated_at":     now,
		})
	if result.Error != nil {
		return fmt.Errorf("update transfer status: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("%w: transfer %s is no longer PENDING", domain.ErrInvalidStatusTransition, transfer.ID)
	}
	transfer.UpdatedAt = now
	return nil
}

func (postgresRepository) GetTransferByIdempotencyKey(db *gorm.DB, key string) (*domain.Transfer, error) {
	var model transferModel
	if err := db.First(&model, "idempotency_key = ?", key).Error; err != nil {
		return nil, fmt.Errorf("get transfer by idempotency key: %w", err)
	}
	return model.toDomain(), nil
}

func nullableReason(reason domain.FailureReason) *string {
	if reason == "" {
		return nil
	}
	value := string(reason)
	return &value
}

func dbNow() time.Time {
	return time.Now().UTC().Truncate(time.Microsecond)
}
