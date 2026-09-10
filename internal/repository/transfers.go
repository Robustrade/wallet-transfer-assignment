package repository

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/google/uuid"
	"github.com/sushmitaw/wallet-transfer-assignment/internal/domain"
)

func (r *DBRepository) CreateTransfer(
	ctx context.Context,
	db *sql.Tx,
	transfer *domain.Transfer,
) error {
	const query = `
		INSERT INTO transfers (
			id,
			from_wallet_id,
			to_wallet_id,
			amount,
			status
		)
		VALUES ($1, $2, $3, $4, $5)
	`

	_, err := db.ExecContext(
		ctx,
		query,
		transfer.ID,
		transfer.FromWalletID,
		transfer.ToWalletID,
		transfer.Amount,
		transfer.Status,
	)
	if err != nil {
		return fmt.Errorf("create transfer: %w", err)
	}

	return nil
}

func (r *DBRepository) UpdateTransferStatus(
	ctx context.Context,
	db *sql.Tx,
	transferID uuid.UUID,
	fromStatus domain.TransferStatus,
	toStatus domain.TransferStatus,
) error {
	const query = `
		UPDATE transfers
		SET status = $1,
		    updated_at = NOW()
		WHERE id = $2
		  AND status = $3
	`

	result, err := db.ExecContext(
		ctx,
		query,
		toStatus,
		transferID,
		fromStatus,
	)
	if err != nil {
		return fmt.Errorf("update transfer status: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("get transfer rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return domain.ErrInvalidTransferState
	}

	return nil
}
