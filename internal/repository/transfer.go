package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/aks-95/wallet-transfer-assignment/internal/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type InsertTransferParams struct {
	IdempotencyKey string
	FromWalletID   uuid.UUID
	ToWalletID     uuid.UUID
	Amount         int64
	Status         domain.TransferStatus
	FailureReason  *string
}

func GetTransferByIdempotencyKey(ctx context.Context, q pgx.Tx, idempotencyKey string) (*domain.Transfer, error) {
	row := q.QueryRow(ctx, `
		SELECT id, idempotency_key, from_wallet_id, to_wallet_id, amount, status, failure_reason, created_at, updated_at
		FROM transfers
		WHERE idempotency_key = $1
	`, idempotencyKey)

	transfer, err := scanTransfer(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get transfer by idempotency key: %w", err)
	}
	return transfer, nil
}

func GetTransfer(ctx context.Context, q pgx.Tx, transferID uuid.UUID) (*domain.Transfer, error) {
	row := q.QueryRow(ctx, `
		SELECT id, idempotency_key, from_wallet_id, to_wallet_id, amount, status, failure_reason, created_at, updated_at
		FROM transfers
		WHERE id = $1
	`, transferID)

	transfer, err := scanTransfer(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get transfer: %w", err)
	}
	return transfer, nil
}

func InsertTransfer(ctx context.Context, tx pgx.Tx, params InsertTransferParams) (*domain.Transfer, error) {
	row := tx.QueryRow(ctx, `
		INSERT INTO transfers (idempotency_key, from_wallet_id, to_wallet_id, amount, status, failure_reason)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, idempotency_key, from_wallet_id, to_wallet_id, amount, status, failure_reason, created_at, updated_at
	`, params.IdempotencyKey, params.FromWalletID, params.ToWalletID, params.Amount, params.Status, params.FailureReason)

	transfer, err := scanTransfer(row)
	if err != nil {
		return nil, fmt.Errorf("insert transfer: %w", err)
	}
	return transfer, nil
}

func UpdateTransferStatus(
	ctx context.Context,
	tx pgx.Tx,
	transferID uuid.UUID,
	status domain.TransferStatus,
	failureReason *string,
) (*domain.Transfer, error) {
	row := tx.QueryRow(ctx, `
		UPDATE transfers
		SET status = $2, failure_reason = $3, updated_at = now()
		WHERE id = $1
		RETURNING id, idempotency_key, from_wallet_id, to_wallet_id, amount, status, failure_reason, created_at, updated_at
	`, transferID, status, failureReason)

	transfer, err := scanTransfer(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("update transfer status: %w", err)
	}
	return transfer, nil
}

func scanTransfer(row scannable) (*domain.Transfer, error) {
	var transfer domain.Transfer
	var status string
	err := row.Scan(
		&transfer.ID,
		&transfer.IdempotencyKey,
		&transfer.FromWalletID,
		&transfer.ToWalletID,
		&transfer.Amount,
		&status,
		&transfer.FailureReason,
		&transfer.CreatedAt,
		&transfer.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	transfer.Status, err = domain.ParseTransferStatus(status)
	if err != nil {
		return nil, err
	}

	return &transfer, nil
}
