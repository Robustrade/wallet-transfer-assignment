package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/prateekgautam/wallet-transfer-assignment/internal/domain"
)

// TransferRepository handles transfer persistence operations.
type TransferRepository struct{}

// NewTransferRepository creates a new TransferRepository.
func NewTransferRepository() *TransferRepository {
	return &TransferRepository{}
}

// Create inserts a new transfer record. The database generates the UUID primary key.
func (r *TransferRepository) Create(ctx context.Context, db DBTX, transfer *domain.Transfer) (*domain.Transfer, error) {
	err := db.QueryRow(ctx,
		`INSERT INTO transfers (idempotency_key, from_wallet_id, to_wallet_id, amount, status, error_reason)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING id::text, created_at, updated_at`,
		transfer.IdempotencyKey, transfer.FromWalletID, transfer.ToWalletID,
		transfer.Amount, transfer.Status, transfer.ErrorReason,
	).Scan(&transfer.ID, &transfer.CreatedAt, &transfer.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return transfer, nil
}

// GetByID retrieves a transfer by its UUID (passed as string, cast in SQL).
func (r *TransferRepository) GetByID(ctx context.Context, db DBTX, id string) (*domain.Transfer, error) {
	transfer := &domain.Transfer{}
	err := db.QueryRow(ctx,
		`SELECT id::text, idempotency_key, from_wallet_id, to_wallet_id,
		        amount, status, error_reason, created_at, updated_at
		 FROM transfers WHERE id = $1::uuid`,
		id,
	).Scan(
		&transfer.ID, &transfer.IdempotencyKey, &transfer.FromWalletID,
		&transfer.ToWalletID, &transfer.Amount, &transfer.Status,
		&transfer.ErrorReason, &transfer.CreatedAt, &transfer.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrTransferNotFound
		}
		return nil, err
	}
	return transfer, nil
}

// UpdateStatus transitions a transfer to a new status with an optional error reason.
func (r *TransferRepository) UpdateStatus(ctx context.Context, db DBTX, id string, status domain.TransferStatus, errorReason *string) error {
	result, err := db.Exec(ctx,
		`UPDATE transfers SET status = $1, error_reason = $2, updated_at = NOW()
		 WHERE id = $3::uuid`,
		status, errorReason, id,
	)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return domain.ErrTransferNotFound
	}
	return nil
}
