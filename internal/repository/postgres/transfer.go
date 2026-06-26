package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/ferozyk/wallet-transfer-assignment/internal/domain"
	"github.com/ferozyk/wallet-transfer-assignment/internal/service"
)

// TransferRepository is the PostgreSQL adapter for transfer persistence.
type TransferRepository struct {
	db *DB
}

// NewTransferRepository constructs a TransferRepository over db.
func NewTransferRepository(db *DB) *TransferRepository {
	return &TransferRepository{db: db}
}

var _ service.TransferRepository = (*TransferRepository)(nil)

// Create inserts a new transfer row.
func (r *TransferRepository) Create(ctx context.Context, transfer *domain.Transfer) error {
	_, err := r.db.conn(ctx).Exec(ctx,
		`INSERT INTO transfers
		   (id, idempotency_key, from_wallet_id, to_wallet_id, amount, status, failure_reason, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, now(), now())`,
		transfer.ID,
		transfer.IdempotencyKey,
		transfer.FromWalletID,
		transfer.ToWalletID,
		transfer.Amount,
		string(transfer.Status),
		transfer.FailureReason,
	)
	if err != nil {
		return fmt.Errorf("postgres: create transfer: %w", err)
	}
	return nil
}

// Get returns a transfer by id, or domain.ErrTransferNotFound.
func (r *TransferRepository) Get(ctx context.Context, id string) (*domain.Transfer, error) {
	var (
		t      domain.Transfer
		status string
	)
	err := r.db.conn(ctx).QueryRow(ctx,
		`SELECT id, idempotency_key, from_wallet_id, to_wallet_id, amount, status, failure_reason, created_at, updated_at
		   FROM transfers WHERE id = $1`,
		id,
	).Scan(
		&t.ID,
		&t.IdempotencyKey,
		&t.FromWalletID,
		&t.ToWalletID,
		&t.Amount,
		&status,
		&t.FailureReason,
		&t.CreatedAt,
		&t.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrTransferNotFound
		}
		return nil, fmt.Errorf("postgres: get transfer: %w", err)
	}
	t.Status = domain.TransferStatus(status)
	return &t, nil
}

// Update persists status and failure-reason changes for a transfer.
func (r *TransferRepository) Update(ctx context.Context, transfer *domain.Transfer) error {
	tag, err := r.db.conn(ctx).Exec(ctx,
		`UPDATE transfers
		    SET status = $2, failure_reason = $3, updated_at = now()
		  WHERE id = $1`,
		transfer.ID,
		string(transfer.Status),
		transfer.FailureReason,
	)
	if err != nil {
		return fmt.Errorf("postgres: update transfer: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrTransferNotFound
	}
	return nil
}
