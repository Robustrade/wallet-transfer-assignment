package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/devthedeveloper/wallet-transfer-assignment/internal/domain"
)

type transferRepository struct {
	q *gorm.DB
}

// InsertIfAbsent performs the speculative insert described in
// docs/design.md §6: if a concurrent transaction holds the same
// idempotency_key uncommitted, this statement blocks until that transaction
// resolves, then either observes the committed row (inserted=false) or
// finds the key free again and inserts (inserted=true) if it rolled back.
// clause.OnConflict is GORM's escape hatch for exactly this — it renders
// literal ON CONFLICT (idempotency_key) DO NOTHING, nothing more clever.
func (r *transferRepository) InsertIfAbsent(ctx context.Context, t domain.Transfer) (bool, error) {
	m := transferModel{
		ID:             t.ID,
		IdempotencyKey: t.IdempotencyKey,
		RequestHash:    t.RequestHash,
		FromWalletID:   t.FromWalletID,
		ToWalletID:     t.ToWalletID,
		AmountMinor:    t.AmountMinor,
		Currency:       t.Currency,
		Status:         string(t.Status),
		CreatedAt:      t.CreatedAt,
		UpdatedAt:      t.CreatedAt,
	}
	tx := r.q.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "idempotency_key"}},
			DoNothing: true,
		}).
		Create(&m)
	if tx.Error != nil {
		return false, translateError(tx.Error)
	}
	return tx.RowsAffected == 1, nil
}

func (r *transferRepository) FindByID(ctx context.Context, id uuid.UUID) (domain.Transfer, error) {
	var m transferModel
	if err := r.q.WithContext(ctx).Where("id = ?", id).Take(&m).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return domain.Transfer{}, domain.ErrTransferNotFound
		}
		return domain.Transfer{}, translateError(err)
	}
	return transferFromModel(m), nil
}

func (r *transferRepository) FindByIdempotencyKey(ctx context.Context, key string) (domain.Transfer, error) {
	var m transferModel
	if err := r.q.WithContext(ctx).Where("idempotency_key = ?", key).Take(&m).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return domain.Transfer{}, domain.ErrTransferNotFound
		}
		return domain.Transfer{}, translateError(err)
	}
	return transferFromModel(m), nil
}

// MarkProcessed and MarkFailed are both guarded by WHERE status = 'PENDING',
// backstopped by the transfers_enforce_invariants trigger. RowsAffected !=
// 1 means the transfer was not PENDING — an invariant violation the caller
// must treat as an error, not silently ignore.
func (r *transferRepository) MarkProcessed(ctx context.Context, id uuid.UUID, settledAt time.Time) error {
	tx := r.q.WithContext(ctx).Model(&transferModel{}).
		Where("id = ? AND status = ?", id, string(domain.TransferStatusPending)).
		Updates(map[string]any{
			"status":     string(domain.TransferStatusProcessed),
			"settled_at": settledAt,
			"updated_at": gorm.Expr("now()"),
		})
	if tx.Error != nil {
		return translateError(tx.Error)
	}
	if tx.RowsAffected != 1 {
		return fmt.Errorf("%w: transfer %s was not PENDING when marking processed", domain.ErrIllegalStateChange, id)
	}
	return nil
}

func (r *transferRepository) MarkFailed(ctx context.Context, id uuid.UUID, code domain.FailureCode, message string) error {
	tx := r.q.WithContext(ctx).Model(&transferModel{}).
		Where("id = ? AND status = ?", id, string(domain.TransferStatusPending)).
		Updates(map[string]any{
			"status":          string(domain.TransferStatusFailed),
			"failure_code":    string(code),
			"failure_message": message,
			"updated_at":      gorm.Expr("now()"),
		})
	if tx.Error != nil {
		return translateError(tx.Error)
	}
	if tx.RowsAffected != 1 {
		return fmt.Errorf("%w: transfer %s was not PENDING when marking failed", domain.ErrIllegalStateChange, id)
	}
	return nil
}

func transferFromModel(m transferModel) domain.Transfer {
	t := domain.Transfer{
		ID:             m.ID,
		IdempotencyKey: m.IdempotencyKey,
		RequestHash:    m.RequestHash,
		FromWalletID:   m.FromWalletID,
		ToWalletID:     m.ToWalletID,
		AmountMinor:    m.AmountMinor,
		Currency:       m.Currency,
		Status:         domain.TransferStatus(m.Status),
		CreatedAt:      m.CreatedAt,
		UpdatedAt:      m.UpdatedAt,
		SettledAt:      m.SettledAt,
	}
	if m.FailureCode != nil {
		t.FailureCode = domain.FailureCode(*m.FailureCode)
	}
	if m.FailureMessage != nil {
		t.FailureMessage = *m.FailureMessage
	}
	return t
}
