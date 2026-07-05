package repository

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/ravindranathreddy/wallet-transfer-assignment/internal/domain"
)

type TransferRepository struct{}

func NewTransferRepository() *TransferRepository {
	return &TransferRepository{}
}

func (r *TransferRepository) GetTransferByID(ctx context.Context, db Executor, transferID uuid.UUID) (domain.Transfer, error) {
	selectQuery := `SELECT id, from_wallet_id, to_wallet_id, amount, status, failure_reason, created_at, updated_at FROM transfers WHERE id = $1`
	var transfer domain.Transfer
	err := db.QueryRow(ctx, selectQuery, transferID).Scan(&transfer.ID, &transfer.FromWalletID, &transfer.ToWalletID, &transfer.Amount, &transfer.Status, &transfer.FailureReason, &transfer.CreatedAt, &transfer.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Transfer{}, domain.ErrTransferNotFound
		}
		return domain.Transfer{}, err
	}
	return transfer, nil
}

func (r *TransferRepository) InsertTransfer(ctx context.Context, db Executor, transfer domain.Transfer) error {
	insertQuery := `INSERT INTO transfers (id, from_wallet_id, to_wallet_id, amount, status, failure_reason, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`
	_, err := db.Exec(ctx, insertQuery, transfer.ID, transfer.FromWalletID, transfer.ToWalletID, transfer.Amount, transfer.Status, transfer.FailureReason, transfer.CreatedAt, transfer.UpdatedAt)
	return err
}

func (r *TransferRepository) UpdateTransferStatus(ctx context.Context, db Executor, transferID uuid.UUID, newStatus domain.TransferStatus, failureReason *string) error {
	updateQuery := `UPDATE transfers SET status = $1, failure_reason = $2, updated_at = NOW() WHERE id = $3`
	_, err := db.Exec(ctx, updateQuery, newStatus, failureReason, transferID)
	return err
}

func (r *TransferRepository) LockForUpdate(ctx context.Context, tx pgx.Tx, transferID uuid.UUID) (domain.Transfer, error) {
	query := `SELECT id, from_wallet_id, to_wallet_id, amount, status, failure_reason, created_at, updated_at FROM transfers WHERE id = $1 FOR UPDATE`
	var transfer domain.Transfer
	err := tx.QueryRow(ctx, query, transferID).Scan(&transfer.ID, &transfer.FromWalletID, &transfer.ToWalletID, &transfer.Amount, &transfer.Status, &transfer.FailureReason, &transfer.CreatedAt, &transfer.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Transfer{}, domain.ErrTransferNotFound
		}
		return domain.Transfer{}, err
	}
	return transfer, nil
}
