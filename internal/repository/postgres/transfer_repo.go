package postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"wallet-transfer/internal/domain"
)

type TransferRepository struct {
	tx *TxManager
}

func NewTransferRepository(tx *TxManager) *TransferRepository { return &TransferRepository{tx: tx} }

func (r *TransferRepository) Create(ctx context.Context, t *domain.Transfer) error {
	const query = `
		INSERT INTO transfers (id, from_wallet_id, to_wallet_id, amount, currency, status, failure_reason)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING created_at, updated_at`

	err := r.tx.executorFrom(ctx).QueryRowContext(ctx, query,
		t.ID, t.FromWalletID, t.ToWalletID, int64(t.Amount), t.Currency, string(t.Status), t.FailureReason,
	).Scan(&t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return fmt.Errorf("create transfer %s: %w", t.ID, translateError(err))
	}
	return nil
}

func (r *TransferRepository) UpdateStatus(ctx context.Context, id uuid.UUID, from, to domain.TransferStatus, reason *string) error {
	const query = `
		UPDATE transfers
		SET status = $3, failure_reason = $4, updated_at = now()
		WHERE id = $1 AND status = $2`

	result, err := r.tx.executorFrom(ctx).ExecContext(ctx, query, id, string(from), string(to), reason)
	if err != nil {
		return fmt.Errorf("update transfer %s status: %w", id, translateError(err))
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("update transfer %s status: %w", id, translateError(err))
	}
	if affected != 1 {
		return fmt.Errorf("%w: transfer %s is no longer %s", domain.ErrInvalidStateTransition, id, from)
	}
	return nil
}
