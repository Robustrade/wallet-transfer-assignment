package postgres

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/trjade/wallet-transfer-service/internal/domain"
)

type TransferRepository struct {
	db *pgxpool.Pool
}

func NewTransferRepository(db *pgxpool.Pool) *TransferRepository {
	return &TransferRepository{db: db}
}

func (r *TransferRepository) getExecutor(ctx context.Context) QueryExecutor {
	tx := extractTx(ctx)
	if tx != nil {
		return tx
	}
	return r.db
}

func (r *TransferRepository) CreateTransfer(ctx context.Context, transfer *domain.Transfer) error {
	query := `
		INSERT INTO transfers (
		id, 
		idempotency_key,
		from_wallet_id, 
		to_wallet_id, 
		amount, 
		transfer_status, 
		created_at, 
		updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, NOW(), NOW())
	`

	_, err := r.getExecutor(ctx).Exec(ctx, query,
		transfer.ID,
		transfer.IdempotencyKey,
		transfer.FromWalletID,
		transfer.ToWalletID,
		transfer.Amount,
		transfer.Status,
	)

	return err
}

func (r *TransferRepository) GetTransferByIdempotencyKey(ctx context.Context, idempotencyKey string) (*domain.Transfer, error) {
	query := `
		SELECT
			id,
			idempotency_key,
			from_wallet_id,
			to_wallet_id,
			amount,
			transfer_status,
			created_at,
			updated_at
		FROM transfers
		WHERE idempotency_key = $1
	`
	var transfer domain.Transfer
	err := r.getExecutor(ctx).QueryRow(ctx, query, idempotencyKey).Scan(
		&transfer.ID,
		&transfer.IdempotencyKey,
		&transfer.FromWalletID,
		&transfer.ToWalletID,
		&transfer.Amount,
		&transfer.Status,
		&transfer.CreatedAt,
		&transfer.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrTransferNotFound
		}
	}

	return &transfer, nil
}

func (r *TransferRepository) UpdateTransferStatus(ctx context.Context, transferID uuid.UUID, newStatus domain.TransferStatus) error {
	query := `
		UPDATE transfers
		SET transfer_status = $2, updated_at = NOW()
		WHERE id = $1
	`
	_, err := r.getExecutor(ctx).Exec(ctx, query, transferID, newStatus)
	if err != nil {
		return err
	}

	return nil
}
