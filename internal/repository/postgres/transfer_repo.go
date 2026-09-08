package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"wallettransfer/internal/domain"
)

// TransferRepo is the PostgreSQL implementation of service.TransferRepo.
type TransferRepo struct {
	db *sql.DB
}

func NewTransferRepo(db *sql.DB) *TransferRepo { return &TransferRepo{db: db} }

func (r *TransferRepo) Insert(ctx context.Context, tx *sql.Tx, t domain.Transfer) error {
	const q = `
		INSERT INTO transfers (id, idempotency_key, from_wallet_id, to_wallet_id, amount, status, failure_reason, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, now(), now())`
	_, err := tx.ExecContext(ctx, q, t.ID, t.IdempotencyKey, t.FromWalletID, t.ToWalletID, t.Amount, t.Status, t.FailureReason)
	if err != nil {
		return fmt.Errorf("insert transfer %s: %w", t.ID, err)
	}
	return nil
}

func (r *TransferRepo) UpdateStatus(ctx context.Context, tx *sql.Tx, transferID string, status domain.TransferStatus, failureReason string) error {
	const q = `
		UPDATE transfers
		SET status = $1, failure_reason = $2, updated_at = now()
		WHERE id = $3 AND status = 'PENDING'` // guard: only PENDING can transition, enforced again at DB level
	res, err := tx.ExecContext(ctx, q, status, failureReason, transferID)
	if err != nil {
		return fmt.Errorf("update transfer status %s: %w", transferID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return domain.ErrInvalidStateTransition
	}
	return nil
}

func (r *TransferRepo) Get(ctx context.Context, transferID string) (domain.Transfer, error) {
	const q = `
		SELECT id, idempotency_key, from_wallet_id, to_wallet_id, amount, status, failure_reason, created_at, updated_at
		FROM transfers WHERE id = $1`
	var t domain.Transfer
	err := r.db.QueryRowContext(ctx, q, transferID).Scan(
		&t.ID, &t.IdempotencyKey, &t.FromWalletID, &t.ToWalletID, &t.Amount, &t.Status, &t.FailureReason, &t.CreatedAt, &t.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Transfer{}, fmt.Errorf("transfer %s not found", transferID)
	}
	if err != nil {
		return domain.Transfer{}, fmt.Errorf("get transfer %s: %w", transferID, err)
	}
	return t, nil
}
