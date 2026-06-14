package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/wallet-transfer-assignment/internal/domain"
)

// TransferRepository handles all persistence operations for transfers.
type TransferRepository struct {
	db DBTX
}

func NewTransferRepository(db DBTX) *TransferRepository {
	return &TransferRepository{db: db}
}

// Create inserts a new transfer record. If the idempotency key already exists
// it returns domain.ErrDuplicateKey so the caller can fetch and return the
// original result instead of failing.
func (r *TransferRepository) Create(ctx context.Context, t *domain.Transfer) error {
	const q = `
		INSERT INTO transfers
		    (id, idempotency_key, from_wallet_id, to_wallet_id, amount, status)
		VALUES
		    ($1, $2, $3, $4, $5, $6)
		RETURNING created_at, updated_at`

	err := r.db.QueryRow(ctx, q,
		t.ID, t.IdempotencyKey, t.FromWalletID, t.ToWalletID, t.Amount, t.Status,
	).Scan(&t.CreatedAt, &t.UpdatedAt)

	if err != nil {
		if isUniqueViolation(err) {
			return domain.ErrDuplicateKey
		}
		return fmt.Errorf("transfer.Create: %w", err)
	}
	return nil
}

// GetByIdempotencyKey looks up a transfer by its idempotency key.
func (r *TransferRepository) GetByIdempotencyKey(ctx context.Context, key string) (*domain.Transfer, error) {
	const q = `
		SELECT id, idempotency_key, from_wallet_id, to_wallet_id,
		       amount, status, COALESCE(failure_reason, ''), created_at, updated_at
		FROM   transfers
		WHERE  idempotency_key = $1`

	t := &domain.Transfer{}
	err := r.db.QueryRow(ctx, q, key).Scan(
		&t.ID, &t.IdempotencyKey, &t.FromWalletID, &t.ToWalletID,
		&t.Amount, &t.Status, &t.FailureReason, &t.CreatedAt, &t.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrTransferNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("transfer.GetByIdempotencyKey: %w", err)
	}
	return t, nil
}

// UpdateStatus persists the final status and optional failure reason.
func (r *TransferRepository) UpdateStatus(ctx context.Context, t *domain.Transfer) error {
	const q = `
		UPDATE transfers
		SET    status         = $1,
		       failure_reason = NULLIF($2, ''),
		       updated_at     = NOW()
		WHERE  id = $3
		RETURNING updated_at`

	err := r.db.QueryRow(ctx, q, t.Status, t.FailureReason, t.ID).Scan(&t.UpdatedAt)
	if err != nil {
		return fmt.Errorf("transfer.UpdateStatus: %w", err)
	}
	return nil
}

// isUniqueViolation returns true when the error is a PostgreSQL unique-
// constraint violation (SQLSTATE 23505).
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
