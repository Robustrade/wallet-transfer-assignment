package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/robustrade/wallet-transfer-assignment/internal/domain"
)

type TransferRepository struct {
	db *db
}

func NewTransferRepository(pool *pgxpool.Pool) *TransferRepository {
	return &TransferRepository{db: &db{pool: pool}}
}

func (r *TransferRepository) Create(ctx context.Context, t *domain.Transfer) error {
	row := r.db.q(ctx).QueryRow(ctx, `
		INSERT INTO transfers
			(id, idempotency_key, from_wallet_id, to_wallet_id, amount, status, failure_reason)
		VALUES ($1, $2, $3, $4, $5, $6, NULLIF($7, ''))
		RETURNING created_at, updated_at
	`, t.ID, t.IdempotencyKey, t.FromWalletID, t.ToWalletID, t.Amount, t.Status, t.FailureReason)

	if err := row.Scan(&t.CreatedAt, &t.UpdatedAt); err != nil {
		return fmt.Errorf("insert transfer: %w", err)
	}
	return nil
}

func (r *TransferRepository) UpdateStatus(ctx context.Context, id string, status domain.TransferStatus, failureReason string) error {
	tag, err := r.db.q(ctx).Exec(ctx, `
		UPDATE transfers
		SET status = $1, failure_reason = NULLIF($2, ''), updated_at = now()
		WHERE id = $3
	`, status, failureReason, id)
	if err != nil {
		return fmt.Errorf("update transfer status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrTransferNotFound
	}
	return nil
}

func (r *TransferRepository) GetByID(ctx context.Context, id string) (*domain.Transfer, error) {
	row := r.db.q(ctx).QueryRow(ctx, `
		SELECT id, idempotency_key, from_wallet_id, to_wallet_id, amount, status,
		       COALESCE(failure_reason, ''), created_at, updated_at
		FROM transfers WHERE id = $1
	`, id)

	var t domain.Transfer
	if err := row.Scan(&t.ID, &t.IdempotencyKey, &t.FromWalletID, &t.ToWalletID, &t.Amount,
		&t.Status, &t.FailureReason, &t.CreatedAt, &t.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrTransferNotFound
		}
		return nil, fmt.Errorf("scan transfer: %w", err)
	}
	return &t, nil
}
