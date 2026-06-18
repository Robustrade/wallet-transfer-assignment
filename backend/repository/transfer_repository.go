package repository

import (
	"context"
	"errors"
	"wallet-transfer/apperrors"
	"wallet-transfer/domain"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type TransferRepository interface {
	Create(
		ctx context.Context,
		tx pgx.Tx,
		transfer *domain.Transfer,
	) error

	GetByIdempotencyKey(
		ctx context.Context,
		key string,
	) (*domain.Transfer, error)

	UpdateStatus(
		ctx context.Context,
		tx pgx.Tx,
		transferID string,
		status domain.TransferStatus,
	) error
}

type transferRepository struct {
	db *pgxpool.Pool
}

func NewTransferRepository(db *pgxpool.Pool) TransferRepository {
	return &transferRepository{db: db}
}

func (r *transferRepository) Create(
	ctx context.Context,
	tx pgx.Tx,
	transfer *domain.Transfer,
) error {

	query := `
	INSERT INTO transfers (
		id,
		idempotency_key,
		from_wallet_id,
		to_wallet_id,
		amount,
		status
	) VALUES ($1, $2, $3, $4, $5, $6)
	`

	_, err := tx.Exec(
		ctx,
		query,
		transfer.ID,
		transfer.IdempotencyKey,
		transfer.FromWalletID,
		transfer.ToWalletID,
		transfer.Amount,
		transfer.Status,
	)

	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return apperrors.ErrDuplicateIdempotencyKey
		}
		return err
	}

	return nil
}

func (r *transferRepository) GetByIdempotencyKey(
	ctx context.Context,
	key string,
) (*domain.Transfer, error) {

	query := `
	SELECT
		id,
		idempotency_key,
		from_wallet_id,
		to_wallet_id,
		amount,
		status
	FROM transfers
	WHERE idempotency_key = $1
	`

	row := r.db.QueryRow(ctx, query, key)

	var t domain.Transfer

	err := row.Scan(
		&t.ID,
		&t.IdempotencyKey,
		&t.FromWalletID,
		&t.ToWalletID,
		&t.Amount,
		&t.Status,
	)
	if err != nil {
		return nil, err
	}

	return &t, nil
}

func (r *transferRepository) UpdateStatus(
	ctx context.Context,
	tx pgx.Tx,
	transferID string,
	status domain.TransferStatus,
) error {

	query := `
	UPDATE transfers
	SET status = $1, updated_at = NOW()
	WHERE id = $2
	`

	_, err := tx.Exec(ctx, query, status, transferID)
	return err
}
