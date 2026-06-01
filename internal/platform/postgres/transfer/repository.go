package transfer

import (
	"context"

	"github.com/yashbaheti1971/wallet-transfer-assignment/internal/domain/transfer"
	"gorm.io/gorm"
)

// Repository implements the transfer.Repository interface using GORM.
type Repository struct {
	db *gorm.DB
}

// New creates a new Transfer repository instance.
func New(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

// Create persists a new transfer row.
func (r *Repository) Create(ctx context.Context, t *transfer.Transfer) error {
	m := FromDomain(t)
	return r.db.WithContext(ctx).Create(m).Error
}

// GetByID fetches a transfer by its internal ID.
func (r *Repository) GetByID(ctx context.Context, id string) (*transfer.Transfer, error) {
	var m Model
	if err := r.db.WithContext(ctx).First(&m, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return m.ToDomain(), nil
}

// GetByTxnID looks up a transfer using the caller‑provided idempotency key.
// Returns (nil, nil) when not found.
func (r *Repository) GetByTxnID(ctx context.Context, txnID string) (*transfer.Transfer, error) {
	var m Model
	err := r.db.WithContext(ctx).Where("txn_id = ?", txnID).First(&m).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return m.ToDomain(), nil
}

// UpdateStatus updates the transfer status (and optional failure reason).
func (r *Repository) UpdateStatus(ctx context.Context, id string, status transfer.Status, failureReason string) error {
	updates := map[string]any{"status": string(status)}
	if failureReason != "" {
		updates["failure_reason"] = failureReason
	}
	return r.db.WithContext(ctx).
		Model(&Model{}).
		Where("id = ?", id).
		Updates(updates).Error
}
