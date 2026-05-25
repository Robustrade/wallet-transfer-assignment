package repository

import (
	"context"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"wallet-transfer-assignment/internal/domain"
)

type TransferRepository struct {
	db *pgxpool.Pool
}

func NewTransferRepository(db *pgxpool.Pool) *TransferRepository {
	return &TransferRepository{db: db}
}

func (r *TransferRepository) Create(ctx context.Context, transfer *domain.Transfer) (*domain.Transfer, error) {
	var created domain.Transfer

	err := r.db.QueryRow(ctx, `
		INSERT INTO transfers (from_wallet_id, to_wallet_id, amount, status)
		VALUES ($1, $2, $3, $4)
		RETURNING transfer_id, from_wallet_id, to_wallet_id, amount, status
	`, transfer.FromWalletID, transfer.ToWalletID, transfer.Amount, transfer.Status).
		Scan(
			&created.ID,
			&created.FromWalletID,
			&created.ToWalletID,
			&created.Amount,
			&created.Status,
		)
	if err != nil {
		if pgErr, ok := err.(*pgconn.PgError); ok {
			if pgErr.Code == "23503" {
				return nil, domain.ErrWalletNotFound
			}

		}

		return nil, err
	}

	return &created, nil
}
