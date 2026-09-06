package postgres

import (
	"context"
	"errors"
	"fmt"

	"wallet-transfer-assignment/internal/domain"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
)

type TransferRepository struct {
	pool *pgxpool.Pool
}

func NewTransferRepository(pool *pgxpool.Pool) *TransferRepository {
	return &TransferRepository{pool: pool}
}

func (r *TransferRepository) CreateTransfer(ctx context.Context, tx pgx.Tx, tr *domain.Transfer) error {
	query := `
		INSERT INTO transfers (
			id, idempotency_key, from_wallet_id, to_wallet_id, amount, status, error_code, error_message, created_at, updated_at, processed_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
	`
	_, err := tx.Exec(ctx, query,
		tr.ID,
		tr.IdempotencyKey,
		tr.FromWalletID,
		tr.ToWalletID,
		tr.Amount.Decimal(),
		tr.Status,
		tr.ErrorCode,
		tr.ErrorMessage,
		tr.CreatedAt,
		tr.UpdatedAt,
		tr.ProcessedAt,
	)
	if err != nil {
		return domain.ErrInternal(fmt.Errorf("failed inserting transfer: %w", err))
	}
	return nil
}

func (r *TransferRepository) GetTransferByID(ctx context.Context, id uuid.UUID) (*domain.Transfer, error) {
	query := `
		SELECT id, idempotency_key, from_wallet_id, to_wallet_id, amount, status, error_code, error_message, created_at, updated_at, processed_at
		FROM transfers
		WHERE id = $1
	`
	var tr domain.Transfer
	var amountDec decimal.Decimal

	err := r.pool.QueryRow(ctx, query, id).Scan(
		&tr.ID,
		&tr.IdempotencyKey,
		&tr.FromWalletID,
		&tr.ToWalletID,
		&amountDec,
		&tr.Status,
		&tr.ErrorCode,
		&tr.ErrorMessage,
		&tr.CreatedAt,
		&tr.UpdatedAt,
		&tr.ProcessedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrTransferNotFound(id.String())
		}
		return nil, domain.ErrInternal(fmt.Errorf("failed fetching transfer: %w", err))
	}
	tr.Amount = domain.NewMoney(amountDec)
	return &tr, nil
}

11
