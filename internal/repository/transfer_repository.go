package repository

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"wallet-transfer-assignment/internal/domain"
)

type TransferRepository struct {
	db *pgxpool.Pool
}

type TransferTx struct {
	tx pgx.Tx
}

func NewTransferRepository(db *pgxpool.Pool) *TransferRepository {
	return &TransferRepository{db: db}
}

func (r *TransferRepository) BeginTx(ctx context.Context) (*TransferTx, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}

	return &TransferTx{tx: tx}, nil
}

func (t *TransferTx) Commit(ctx context.Context) error {
	return t.tx.Commit(ctx)
}

func (t *TransferTx) Rollback(ctx context.Context) error {
	return t.tx.Rollback(ctx)
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

func (t *TransferTx) CreateTransfer(ctx context.Context, transfer *domain.Transfer) (*domain.Transfer, error) {
	var created domain.Transfer

	err := t.tx.QueryRow(ctx, `
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

func (t *TransferTx) UpdateTransferStatus(ctx context.Context, transferID domain.TransferID, status domain.TransferStatus) error {
	tag, err := t.tx.Exec(ctx, `
		UPDATE transfers
		SET status = $1, updated_at = now()
		WHERE transfer_id = $2
	`, status, transferID)
	if err != nil {
		return err
	}

	if tag.RowsAffected() == 0 {
		return domain.ErrTransferNotFound
	}

	return nil
}
