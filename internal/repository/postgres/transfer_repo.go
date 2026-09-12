package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"wallet-transfer-assignment/internal/domain"
	"wallet-transfer-assignment/internal/repository"
)

type transferRepository struct {
	db DBTX
}

// NewTransferRepository returns a repository.TransferRepository backed by PostgreSQL.
func NewTransferRepository(db DBTX) repository.TransferRepository {
	return &transferRepository{db: db}
}

func (r *transferRepository) CreateTransfer(ctx context.Context, t *domain.Transfer) error {
	now := time.Now().UTC()
	if t.CreatedAt.IsZero() {
		t.CreatedAt = now
	}
	t.UpdatedAt = now

	query := `
		INSERT INTO transfers (id, idempotency_key, from_wallet_id, to_wallet_id, amount, status, failure_reason, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`
	_, err := r.db.Exec(ctx, query,
		t.ID,
		t.IdempotencyKey,
		t.FromWalletID,
		t.ToWalletID,
		t.Amount,
		string(t.Status),
		t.FailureReason,
		t.CreatedAt,
		t.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to create transfer: %w", err)
	}
	return nil
}

func (r *transferRepository) GetTransferByID(ctx context.Context, id string) (*domain.Transfer, error) {
	query := `
		SELECT id, idempotency_key, from_wallet_id, to_wallet_id, amount, status, failure_reason, created_at, updated_at
		FROM transfers
		WHERE id = $1
	`
	var t domain.Transfer
	var statusStr string
	err := r.db.QueryRow(ctx, query, id).Scan(
		&t.ID,
		&t.IdempotencyKey,
		&t.FromWalletID,
		&t.ToWalletID,
		&t.Amount,
		&statusStr,
		&t.FailureReason,
		&t.CreatedAt,
		&t.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrTransferNotFound
		}
		return nil, fmt.Errorf("failed to get transfer: %w", err)
	}
	t.Status = domain.TransferStatus(statusStr)
	return &t, nil
}

func (r *transferRepository) GetTransferByIdempotencyKey(ctx context.Context, key string) (*domain.Transfer, error) {
	query := `
		SELECT id, idempotency_key, from_wallet_id, to_wallet_id, amount, status, failure_reason, created_at, updated_at
		FROM transfers
		WHERE idempotency_key = $1
	`
	var t domain.Transfer
	var statusStr string
	err := r.db.QueryRow(ctx, query, key).Scan(
		&t.ID,
		&t.IdempotencyKey,
		&t.FromWalletID,
		&t.ToWalletID,
		&t.Amount,
		&statusStr,
		&t.FailureReason,
		&t.CreatedAt,
		&t.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil // Not found, no error
		}
		return nil, fmt.Errorf("failed to get transfer by idempotency key: %w", err)
	}
	t.Status = domain.TransferStatus(statusStr)
	return &t, nil
}

func (r *transferRepository) UpdateTransferStatus(ctx context.Context, id string, status domain.TransferStatus, failureReason *string) error {
	query := `
		UPDATE transfers
		SET status = $1, failure_reason = $2, updated_at = $3
		WHERE id = $4 AND status = 'PENDING'
	`
	cmdTag, err := r.db.Exec(ctx, query, string(status), failureReason, time.Now().UTC(), id)
	if err != nil {
		return fmt.Errorf("failed to update transfer status: %w", err)
	}
	if cmdTag.RowsAffected() == 0 {
		var existingStatus string
		checkErr := r.db.QueryRow(ctx, "SELECT status FROM transfers WHERE id = $1", id).Scan(&existingStatus)
		if checkErr != nil {
			if errors.Is(checkErr, pgx.ErrNoRows) {
				return domain.ErrTransferNotFound
			}
			return fmt.Errorf("failed to verify transfer existence: %w", checkErr)
		}
		return domain.ErrInvalidStateTransition
	}
	return nil
}
