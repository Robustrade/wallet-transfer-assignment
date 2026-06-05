package repository

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"wallet-transfer-service/internal/domain"
)

type transferRepo struct{}

func NewTransferRepository() TransferRepository {
	return &transferRepo{}
}

func (r *transferRepo) CreateIfNotExists(
	ctx context.Context,
	tx pgx.Tx,
	transfer *domain.Transfer,
) (bool, error) {

	query := `
	INSERT INTO transfers(
		id,
		idempotency_key,
		from_wallet_id,
		to_wallet_id,
		amount,
		state
	)
	VALUES($1,$2,$3,$4,$5,$6)
	ON CONFLICT(idempotency_key)
	DO NOTHING
	`

	tag, err := tx.Exec(
		ctx,
		query,
		transfer.ID,
		transfer.IdempotencyKey,
		transfer.FromWalletID,
		transfer.ToWalletID,
		transfer.Amount,
		transfer.State,
	)

	if err != nil {
		return false, err
	}

	return tag.RowsAffected() == 1, nil
}

func (r *transferRepo) GetByIdempotencyKey(
	ctx context.Context,
	tx pgx.Tx,
	key string,
) (*domain.Transfer, error) {

	query := `
	SELECT
		id,
		idempotency_key,
		from_wallet_id,
		to_wallet_id,
		amount,
		state
	FROM transfers
	WHERE idempotency_key=$1
	`

	var t domain.Transfer

	err := tx.QueryRow(
		ctx,
		query,
		key,
	).Scan(
		&t.ID,
		&t.IdempotencyKey,
		&t.FromWalletID,
		&t.ToWalletID,
		&t.Amount,
		&t.State,
	)

	if err != nil {
		return nil, err
	}

	return &t, nil
}

func (r *transferRepo) GetByID(
	ctx context.Context,
	tx pgx.Tx,
	id uuid.UUID,
) (*domain.Transfer, error) {

	query := `
	SELECT
		id,
		idempotency_key,
		from_wallet_id,
		to_wallet_id,
		amount,
		state
	FROM transfers
	WHERE id=$1
	`

	var t domain.Transfer

	err := tx.QueryRow(
		ctx,
		query,
		id,
	).Scan(
		&t.ID,
		&t.IdempotencyKey,
		&t.FromWalletID,
		&t.ToWalletID,
		&t.Amount,
		&t.State,
	)

	if err != nil {
		return nil, err
	}

	return &t, nil
}

func (r *transferRepo) UpdateState(
	ctx context.Context,
	tx pgx.Tx,
	transferID uuid.UUID,
	state domain.TransferState,
) error {

	_, err := tx.Exec(
		ctx,
		`
		UPDATE transfers
		SET state=$1,
		    updated_at=NOW()
		WHERE id=$2
		`,
		state,
		transferID,
	)

	return err
}
