package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/benjamin-benny/wallet-transfer/internal/domain"
)

type TransferRepository struct{}

// Create inserts a transfer row with PENDING status and populates generated fields.
func (r *TransferRepository) Create(ctx context.Context, tx pgx.Tx, t *domain.Transfer) error {
	const q = `
		INSERT INTO transfers (from_wallet_id, to_wallet_id, amount)
		VALUES ($1, $2, $3)
		RETURNING id, status, created_at`

	row := tx.QueryRow(ctx, q,
		pgtype.UUID{Bytes: t.FromWalletID, Valid: true},
		pgtype.UUID{Bytes: t.ToWalletID, Valid: true},
		amountToNumeric(t.Amount),
	)

	var (
		pgID      pgtype.UUID
		statusStr string
	)
	if err := row.Scan(&pgID, &statusStr, &t.CreatedAt); err != nil {
		return fmt.Errorf("TransferRepository.Create scan: %w", err)
	}
	t.ID = uuid.UUID(pgID.Bytes)
	t.Status = domain.TransferStatus(statusStr)
	return nil
}

// MarkProcessed sets status = PROCESSED and processed_at on the transfer.
func (r *TransferRepository) MarkProcessed(ctx context.Context, tx pgx.Tx, id uuid.UUID, processedAt time.Time) error {
	const q = `
		UPDATE transfers
		SET status = 'PROCESSED', processed_at = $2
		WHERE id = $1`

	tag, err := tx.Exec(ctx, q, pgtype.UUID{Bytes: id, Valid: true}, processedAt)
	if err != nil {
		return fmt.Errorf("TransferRepository.MarkProcessed: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("transfer %s not found", id)
	}
	return nil
}

// MarkFailed sets status = FAILED and records the failure reason.
func (r *TransferRepository) MarkFailed(ctx context.Context, tx pgx.Tx, id uuid.UUID, reason string) error {
	const q = `
		UPDATE transfers
		SET status = 'FAILED', failure_reason = $2
		WHERE id = $1`

	tag, err := tx.Exec(ctx, q, pgtype.UUID{Bytes: id, Valid: true}, reason)
	if err != nil {
		return fmt.Errorf("TransferRepository.MarkFailed: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("transfer %s not found", id)
	}
	return nil
}
