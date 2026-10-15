package repository

import (
	"context"
	"errors"

	"github.com/RKShukla1997/wallet-transfer-assignment/internal/domain"
	"github.com/jackc/pgx/v5"
)

// TransferRepository handles persistence for transfers. All business rules
// about when a transfer may move between states live in the service layer;
// this repository only executes the SQL it is told to.
type TransferRepository struct{}

func NewTransferRepository() *TransferRepository {
	return &TransferRepository{}
}

// InsertIfAbsent is the heart of the idempotency guarantee: it attempts to
// insert a new PENDING transfer for the given idempotency key. If a
// transfer with that key already exists, the unique index causes the
// conflict clause to fire and no row is returned; the caller must then
// fetch and return the existing transfer instead of re-running the
// transfer workflow.
func (r *TransferRepository) InsertIfAbsent(ctx context.Context, tx DBTX, t domain.Transfer) (domain.Transfer, bool, error) {
	row := tx.QueryRow(ctx, `
		INSERT INTO transfers (idempotency_key, from_wallet_id, to_wallet_id, amount, status)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (idempotency_key) DO NOTHING
		RETURNING id, idempotency_key, from_wallet_id, to_wallet_id, amount, status, failure_reason, created_at, updated_at
	`, t.IdempotencyKey, t.FromWalletID, t.ToWalletID, t.Amount, domain.TransferStatusPending)

	inserted, err := scanTransfer(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Transfer{}, false, nil
		}
		return domain.Transfer{}, false, err
	}
	return inserted, true, nil
}

// GetByIdempotencyKey fetches a transfer by its idempotency key, used to
// serve replayed/duplicate requests their original result.
func (r *TransferRepository) GetByIdempotencyKey(ctx context.Context, tx DBTX, key string) (domain.Transfer, error) {
	row := tx.QueryRow(ctx, `
		SELECT id, idempotency_key, from_wallet_id, to_wallet_id, amount, status, failure_reason, created_at, updated_at
		FROM transfers
		WHERE idempotency_key = $1
	`, key)
	return scanTransfer(row)
}

// GetByID fetches a transfer by its primary key.
func (r *TransferRepository) GetByID(ctx context.Context, tx DBTX, id string) (domain.Transfer, error) {
	row := tx.QueryRow(ctx, `
		SELECT id, idempotency_key, from_wallet_id, to_wallet_id, amount, status, failure_reason, created_at, updated_at
		FROM transfers
		WHERE id = $1
	`, id)
	return scanTransfer(row)
}

// MarkProcessed transitions a transfer to PROCESSED.
func (r *TransferRepository) MarkProcessed(ctx context.Context, tx DBTX, id string) (domain.Transfer, error) {
	row := tx.QueryRow(ctx, `
		UPDATE transfers
		SET status = $2, updated_at = now()
		WHERE id = $1
		RETURNING id, idempotency_key, from_wallet_id, to_wallet_id, amount, status, failure_reason, created_at, updated_at
	`, id, domain.TransferStatusProcessed)
	return scanTransfer(row)
}

// MarkFailed transitions a transfer to FAILED with a reason.
func (r *TransferRepository) MarkFailed(ctx context.Context, tx DBTX, id string, reason string) (domain.Transfer, error) {
	row := tx.QueryRow(ctx, `
		UPDATE transfers
		SET status = $2, failure_reason = $3, updated_at = now()
		WHERE id = $1
		RETURNING id, idempotency_key, from_wallet_id, to_wallet_id, amount, status, failure_reason, created_at, updated_at
	`, id, domain.TransferStatusFailed, reason)
	return scanTransfer(row)
}

func scanTransfer(row pgx.Row) (domain.Transfer, error) {
	var t domain.Transfer
	var failureReason *string
	err := row.Scan(
		&t.ID, &t.IdempotencyKey, &t.FromWalletID, &t.ToWalletID,
		&t.Amount, &t.Status, &failureReason, &t.CreatedAt, &t.UpdatedAt,
	)
	if err != nil {
		return domain.Transfer{}, err
	}
	if failureReason != nil {
		t.FailureReason = *failureReason
	}
	return t, nil
}
