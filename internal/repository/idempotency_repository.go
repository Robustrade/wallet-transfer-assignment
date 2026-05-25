package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"wallet-transfer-assignment/internal/domain"
)

func (t *TransferTx) GetTransferByIdempotencyKey(ctx context.Context, key string) (*domain.Transfer, error) {
	var transfer domain.Transfer

	err := t.tx.QueryRow(ctx, `
		SELECT tr.transfer_id, tr.from_wallet_id, tr.to_wallet_id, tr.amount, tr.status
		FROM idempotency_records ir
		JOIN transfers tr ON tr.transfer_id = ir.transfer_id
		WHERE ir.idempotency_key = $1
	`, key).Scan(
		&transfer.ID,
		&transfer.FromWalletID,
		&transfer.ToWalletID,
		&transfer.Amount,
		&transfer.Status,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	return &transfer, nil
}

func (t *TransferTx) SaveIdempotencyRecord(ctx context.Context, key string, transferID domain.TransferID) error {
	_, err := t.tx.Exec(ctx, `
		INSERT INTO idempotency_records (idempotency_key, transfer_id)
		VALUES ($1, $2)
	`, key, transferID)
	if err != nil {
		if pgErr, ok := err.(*pgconn.PgError); ok {
			if pgErr.Code == "23503" {
				return domain.ErrTransferNotFound
			}
		}

		return err
	}

	return nil
}
