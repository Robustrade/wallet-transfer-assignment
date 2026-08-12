package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/Swapnil811/wallet-transfer-assignment/wallet-transfer-service/internal/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type TransferRepository struct{}

func NewTransferRepository() *TransferRepository { return &TransferRepository{} }

func (r *TransferRepository) Create(ctx context.Context, db DBTX, t domain.Transfer) error {
	const q = `
        INSERT INTO transfers
            (id, from_wallet_id, to_wallet_id, amount, status, created_at, updated_at)
        VALUES ($1, $2, $3, $4, $5, $6, $6)`

	_, err := db.Exec(ctx, q, t.ID, t.FromWalletID, t.ToWalletID, t.Amount, t.Status, t.CreatedAt)
	if err != nil {
		return fmt.Errorf("create transfer: %w", err)
	}
	return nil
}

func (r *TransferRepository) GetByID(ctx context.Context, db DBTX, id uuid.UUID) (domain.Transfer, error) {
	const q = `
        SELECT id, from_wallet_id, to_wallet_id, amount, status, created_at, updated_at
        FROM transfers
        WHERE id = $1`

	var t domain.Transfer
	err := db.QueryRow(ctx, q, id).Scan(
		&t.ID, &t.FromWalletID, &t.ToWalletID, &t.Amount,
		&t.Status, &t.CreatedAt, &t.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Transfer{}, fmt.Errorf("transfer %s not found", id)
	}
	if err != nil {
		return domain.Transfer{}, fmt.Errorf("get transfer: %w", err)
	}
	return t, nil
}

func (r *TransferRepository) UpdateStatus(ctx context.Context, db DBTX, id uuid.UUID, from, to domain.TransferStatus) error {
	const q = `
        UPDATE transfers
        SET status = $3, updated_at = NOW()
        WHERE id = $1 AND status = $2`

	tag, err := db.Exec(ctx, q, id, from, to)
	if err != nil {
		return fmt.Errorf("update transfer status: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("invalid transfer state transition %s -> %s for %s", from, to, id)
	}
	return nil
}
