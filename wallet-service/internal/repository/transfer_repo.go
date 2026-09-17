package repository

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/shubh/wallet-service/internal/db"
	"github.com/shubh/wallet-service/internal/domain"
)

// TransferRepository handles transfer persistence operations.
type TransferRepository struct {
	db *sql.DB
}

// NewTransferRepository creates a new TransferRepository.
func NewTransferRepository(db *sql.DB) *TransferRepository {
	return &TransferRepository{db: db}
}

// FindByIdempotencyKey looks up an existing transfer by its idempotency key.
// Returns nil, nil if not found.
func (r *TransferRepository) FindByIdempotencyKey(ctx context.Context, key string) (*domain.Transfer, error) {
	dbtx := db.GetTxFromContext(ctx, r.db)
	t := &domain.Transfer{}
	var failureReason sql.NullString

	err := dbtx.QueryRowContext(ctx,
		`SELECT id, idempotency_key, from_wallet_id, to_wallet_id, amount, status, failure_reason, created_at, updated_at
		 FROM transfers
		 WHERE idempotency_key = $1`,
		key,
	).Scan(&t.ID, &t.IdempotencyKey, &t.FromWalletID, &t.ToWalletID, &t.Amount, &t.Status, &failureReason, &t.CreatedAt, &t.UpdatedAt)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to find transfer by idempotency key: %w", err)
	}

	if failureReason.Valid {
		t.FailureReason = &failureReason.String
	}
	return t, nil
}

// Create inserts a new transfer record within a transaction.
func (r *TransferRepository) Create(ctx context.Context, t *domain.Transfer) error {
	dbtx := db.GetTxFromContext(ctx, r.db)
	err := dbtx.QueryRowContext(ctx,
		`INSERT INTO transfers (idempotency_key, from_wallet_id, to_wallet_id, amount, status, failure_reason)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING id, created_at, updated_at`,
		t.IdempotencyKey, t.FromWalletID, t.ToWalletID, t.Amount, t.Status, t.FailureReason,
	).Scan(&t.ID, &t.CreatedAt, &t.UpdatedAt)

	if err != nil {
		return fmt.Errorf("failed to create transfer: %w", err)
	}
	return nil
}

// GetByID retrieves a transfer by its ID (used for read-only lookups outside transactions).
func (r *TransferRepository) GetByID(ctx context.Context, id string) (*domain.Transfer, error) {
	t := &domain.Transfer{}
	var failureReason sql.NullString

	err := r.db.QueryRowContext(ctx,
		`SELECT id, idempotency_key, from_wallet_id, to_wallet_id, amount, status, failure_reason, created_at, updated_at
		 FROM transfers
		 WHERE id = $1`,
		id,
	).Scan(&t.ID, &t.IdempotencyKey, &t.FromWalletID, &t.ToWalletID, &t.Amount, &t.Status, &failureReason, &t.CreatedAt, &t.UpdatedAt)

	if err == sql.ErrNoRows {
		return nil, domain.ErrTransferNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get transfer: %w", err)
	}

	if failureReason.Valid {
		t.FailureReason = &failureReason.String
	}
	return t, nil
}
