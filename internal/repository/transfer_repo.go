package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/mayankkandpal18/wallet-transfer-assignment/internal/domain"
)

// idempotencyKeyConstraint is the Postgres-generated name for the UNIQUE
// constraint on transfers.idempotency_key (<table>_<column>_key).
const idempotencyKeyConstraint = "transfers_idempotency_key_key"

const transferColumns = `id::text, idempotency_key, from_wallet_id, to_wallet_id, ` +
	`amount, status, COALESCE(failure_reason, ''), created_at, updated_at`

// TransferRepository reads and writes the transfers table.
type TransferRepository struct{}

func NewTransferRepository() *TransferRepository { return &TransferRepository{} }

// InsertPending creates the transfer row in PENDING state. The UNIQUE
// constraint on idempotency_key fires here for a duplicate request; that is
// mapped to domain.ErrDuplicateIdempotencyKey so the service can return the
// existing transfer instead.
func (r *TransferRepository) InsertPending(ctx context.Context, db DBTX, t domain.Transfer) (domain.Transfer, error) {
	const q = `INSERT INTO transfers (idempotency_key, from_wallet_id, to_wallet_id, amount, status)
	           VALUES ($1, $2, $3, $4, $5)
	           RETURNING ` + transferColumns
	out, err := scanTransfer(db.QueryRow(ctx, q,
		t.IdempotencyKey, t.FromWalletID, t.ToWalletID, t.Amount, domain.TransferPending))
	if err != nil {
		if isUniqueViolation(err, idempotencyKeyConstraint) {
			return domain.Transfer{}, domain.ErrDuplicateIdempotencyKey
		}
		return domain.Transfer{}, fmt.Errorf("insert pending transfer: %w", err)
	}
	return out, nil
}

// GetByIdempotencyKey returns the transfer for a key, or
// domain.ErrTransferNotFound if none exists.
func (r *TransferRepository) GetByIdempotencyKey(ctx context.Context, db DBTX, key string) (domain.Transfer, error) {
	const q = `SELECT ` + transferColumns + ` FROM transfers WHERE idempotency_key = $1`
	return scanTransfer(db.QueryRow(ctx, q, key))
}

// GetByID returns the transfer for an id, or domain.ErrTransferNotFound.
func (r *TransferRepository) GetByID(ctx context.Context, db DBTX, id string) (domain.Transfer, error) {
	const q = `SELECT ` + transferColumns + ` FROM transfers WHERE id = $1`
	return scanTransfer(db.QueryRow(ctx, q, id))
}

// UpdateStatus moves a transfer to a terminal state and stamps updated_at.
// An empty failureReason is stored as NULL.
func (r *TransferRepository) UpdateStatus(ctx context.Context, db DBTX, id string, status domain.TransferStatus, failureReason string) error {
	const q = `UPDATE transfers
	           SET status = $2, failure_reason = NULLIF($3, ''), updated_at = now()
	           WHERE id = $1`
	ct, err := db.Exec(ctx, q, id, status, failureReason)
	if err != nil {
		return fmt.Errorf("update transfer status: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return domain.ErrTransferNotFound
	}
	return nil
}

func scanTransfer(row pgx.Row) (domain.Transfer, error) {
	var t domain.Transfer
	err := row.Scan(&t.ID, &t.IdempotencyKey, &t.FromWalletID, &t.ToWalletID,
		&t.Amount, &t.Status, &t.FailureReason, &t.CreatedAt, &t.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Transfer{}, domain.ErrTransferNotFound
	}
	if err != nil {
		return domain.Transfer{}, fmt.Errorf("scan transfer: %w", err)
	}
	return t, nil
}
