package repository

import (
	"context"
	"errors"

	"wallet-transfer/internal/db"
	"wallet-transfer/internal/domain"

	"github.com/jackc/pgx/v5"
)

// TransferRepository handles all persistence operations for transfers.
type TransferRepository struct{}

func NewTransferRepository() *TransferRepository {
	return &TransferRepository{}
}

// CreateIfNotExists attempts to insert a transfer record, relying on the
// UNIQUE constraint on idempotency_key for deduplication.
//
// Returns:
//   - (transfer, true, nil)  — record was newly created
//   - (transfer, false, nil) — idempotency_key already existed; existing record returned
//   - (nil, false, err)      — unexpected database error
func (r *TransferRepository) CreateIfNotExists(
	ctx context.Context,
	dbtx db.DBTX,
	t *domain.Transfer,
) (*domain.Transfer, bool, error) {
	const q = `
		INSERT INTO transfers (id, idempotency_key, from_wallet_id, to_wallet_id, amount, status)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (idempotency_key) DO NOTHING
		RETURNING id, idempotency_key, from_wallet_id, to_wallet_id, amount, status, failure_reason, created_at, updated_at`

	var result domain.Transfer
	err := dbtx.QueryRow(ctx, q,
		t.ID, t.IdempotencyKey, t.FromWalletID, t.ToWalletID, t.Amount, t.Status,
	).Scan(
		&result.ID, &result.IdempotencyKey, &result.FromWalletID, &result.ToWalletID,
		&result.Amount, &result.Status, &result.FailureReason, &result.CreatedAt, &result.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// ON CONFLICT DO NOTHING fired — fetch the existing record and return it.
			existing, err := r.getByIdempotencyKey(ctx, dbtx, t.IdempotencyKey)
			if err != nil {
				return nil, false, err
			}
			return existing, false, nil
		}
		return nil, false, err
	}
	return &result, true, nil
}

// UpdateStatus transitions a transfer to a new status and, for FAILED transfers,
// records the human-readable reason.
func (r *TransferRepository) UpdateStatus(
	ctx context.Context,
	dbtx db.DBTX,
	id string,
	status domain.TransferStatus,
	reason string,
) error {
	var reasonPtr *string
	if reason != "" {
		reasonPtr = &reason
	}
	const q = `UPDATE transfers SET status = $1, failure_reason = $2, updated_at = NOW() WHERE id = $3`
	_, err := dbtx.Exec(ctx, q, status, reasonPtr, id)
	return err
}

// GetByWalletID returns all transfers involving the given wallet (as source or destination),
// ordered newest first.
func (r *TransferRepository) GetByWalletID(ctx context.Context, dbtx db.DBTX, walletID string) ([]domain.Transfer, error) {
	const q = `
		SELECT id, idempotency_key, from_wallet_id, to_wallet_id, amount, status, failure_reason, created_at, updated_at
		FROM transfers
		WHERE from_wallet_id = $1 OR to_wallet_id = $1
		ORDER BY created_at DESC`

	rows, err := dbtx.Query(ctx, q, walletID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var transfers []domain.Transfer
	for rows.Next() {
		var t domain.Transfer
		if err := rows.Scan(
			&t.ID, &t.IdempotencyKey, &t.FromWalletID, &t.ToWalletID,
			&t.Amount, &t.Status, &t.FailureReason, &t.CreatedAt, &t.UpdatedAt,
		); err != nil {
			return nil, err
		}
		transfers = append(transfers, t)
	}
	return transfers, rows.Err()
}

func (r *TransferRepository) getByIdempotencyKey(ctx context.Context, dbtx db.DBTX, key string) (*domain.Transfer, error) {
	const q = `
		SELECT id, idempotency_key, from_wallet_id, to_wallet_id, amount, status, failure_reason, created_at, updated_at
		FROM transfers WHERE idempotency_key = $1`

	var t domain.Transfer
	err := dbtx.QueryRow(ctx, q, key).Scan(
		&t.ID, &t.IdempotencyKey, &t.FromWalletID, &t.ToWalletID,
		&t.Amount, &t.Status, &t.FailureReason, &t.CreatedAt, &t.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrTransferNotFound
		}
		return nil, err
	}
	return &t, nil
}
