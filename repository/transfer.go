package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/Robustrade/wallet-transfer-assignment/helpers"
	"github.com/Robustrade/wallet-transfer-assignment/model"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TransferRepository is the DB access for transfers. Business rules live in service.
type TransferRepository struct {
	pool *pgxpool.Pool
}

func NewTransferRepository(pool *pgxpool.Pool) *TransferRepository {
	return &TransferRepository{pool: pool}
}

// Insert persists a transfer row as provided by the caller.
func (r *TransferRepository) Insert(ctx context.Context, t *model.Transfer) error {
	q := QuerierFrom(ctx, r.pool)
	_, err := q.Exec(ctx, `
		INSERT INTO transfers (
			id, idempotency_key, from_wallet_id, to_wallet_id,
			amount, status, failure_reason, created_at, updated_at
		) VALUES (
			@id, @idempotency_key, @from_wallet_id, @to_wallet_id,
			@amount, @status, @failure_reason, @created_at, @updated_at
		)`,
		pgx.NamedArgs{
			"id":              t.ID,
			"idempotency_key": t.IdempotencyKey,
			"from_wallet_id":  t.FromWalletID,
			"to_wallet_id":    t.ToWalletID,
			"amount":          t.Amount.Int64(),
			"status":          string(t.Status),
			"failure_reason":  t.FailureReason,
			"created_at":      t.CreatedAt,
			"updated_at":      t.UpdatedAt,
		},
	)
	if err != nil {
		return fmt.Errorf("insert transfer: %w", err)
	}
	return nil
}

// UpdateStatus persists status and failure_reason from the domain object.
func (r *TransferRepository) UpdateStatus(ctx context.Context, t *model.Transfer) error {
	q := QuerierFrom(ctx, r.pool)
	tag, err := q.Exec(ctx, `
		UPDATE transfers
		SET status = @status, failure_reason = @failure_reason, updated_at = @updated_at
		WHERE id = @id`,
		pgx.NamedArgs{
			"id":             t.ID,
			"status":         string(t.Status),
			"failure_reason": t.FailureReason,
			"updated_at":     t.UpdatedAt,
		},
	)
	if err != nil {
		return fmt.Errorf("update transfer status: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return model.ErrNotFound
	}
	return nil
}

// GetByID loads a transfer or model.ErrNotFound.
func (r *TransferRepository) GetByID(ctx context.Context, id uuid.UUID) (*model.Transfer, error) {
	q := QuerierFrom(ctx, r.pool)
	return scanTransfer(q.QueryRow(ctx, `
		SELECT id, idempotency_key, from_wallet_id, to_wallet_id,
		       amount, status, failure_reason, created_at, updated_at
		FROM transfers WHERE id = @id`,
		pgx.NamedArgs{"id": id},
	))
}

// GetByIdempotencyKey loads the transfer for a key or model.ErrNotFound.
func (r *TransferRepository) GetByIdempotencyKey(ctx context.Context, key string) (*model.Transfer, error) {
	q := QuerierFrom(ctx, r.pool)
	return scanTransfer(q.QueryRow(ctx, `
		SELECT id, idempotency_key, from_wallet_id, to_wallet_id,
		       amount, status, failure_reason, created_at, updated_at
		FROM transfers WHERE idempotency_key = @idempotency_key`,
		pgx.NamedArgs{"idempotency_key": key},
	))
}

func scanTransfer(row pgx.Row) (*model.Transfer, error) {
	var t model.Transfer
	var amount int64
	var status string
	err := row.Scan(
		&t.ID, &t.IdempotencyKey, &t.FromWalletID, &t.ToWalletID,
		&amount, &status, &t.FailureReason, &t.CreatedAt, &t.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, model.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan transfer: %w", err)
	}
	t.Amount = helpers.Money(amount)
	t.Status = model.Status(status)
	return &t, nil
}
