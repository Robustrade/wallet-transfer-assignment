package repository

import (
	"context"
	"database/sql"
	"fmt"

	"wallet-transfer/internal/transfer/domain"
)

// TransferRepository handles transfer database operations
type TransferRepository struct {
	db *sql.DB
}

// NewTransferRepository creates a new transfer repository
func NewTransferRepository(db *sql.DB) *TransferRepository {
	return &TransferRepository{db: db}
}

// Create inserts a new transfer
func (r *TransferRepository) Create(ctx context.Context, tx *sql.Tx, transfer *domain.Transfer) error {
	query := `
		INSERT INTO transfers (transfer_id, from_wallet_id, to_wallet_id, amount_cents, status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`
	_, err := tx.ExecContext(ctx, query,
		transfer.ID,
		transfer.FromWalletID,
		transfer.ToWalletID,
		transfer.Amount,
		transfer.Status,
		transfer.CreatedAt,
		transfer.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to create transfer: %w", err)
	}
	return nil
}

// GetByID retrieves a transfer by ID
func (r *TransferRepository) GetByID(ctx context.Context, tx *sql.Tx, transferID string) (*domain.Transfer, error) {
	query := `
		SELECT transfer_id, from_wallet_id, to_wallet_id, amount_cents, status, error_reason, created_at, updated_at
		FROM transfers
		WHERE transfer_id = $1
	`
	var transfer domain.Transfer
	var errorReason sql.NullString

	row := tx.QueryRowContext(ctx, query, transferID)
	err := row.Scan(
		&transfer.ID,
		&transfer.FromWalletID,
		&transfer.ToWalletID,
		&transfer.Amount,
		&transfer.Status,
		&errorReason,
		&transfer.CreatedAt,
		&transfer.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get transfer: %w", err)
	}

	if errorReason.Valid {
		transfer.ErrorReason = &errorReason.String
	}

	return &transfer, nil
}

// UpdateStatus updates transfer status
func (r *TransferRepository) UpdateStatus(ctx context.Context, tx *sql.Tx, transferID string, status domain.TransferStatus, errorReason *string) error {
	query := `
		UPDATE transfers
		SET status = $1, error_reason = $2, updated_at = NOW()
		WHERE transfer_id = $3
	`
	result, err := tx.ExecContext(ctx, query, status, errorReason, transferID)
	if err != nil {
		return fmt.Errorf("failed to update transfer status: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("transfer not found")
	}
	return nil
}

// ListByWallet retrieves all transfers for a wallet
func (r *TransferRepository) ListByWallet(ctx context.Context, tx *sql.Tx, walletID string, limit int, offset int) ([]domain.Transfer, error) {
	query := `
		SELECT transfer_id, from_wallet_id, to_wallet_id, amount_cents, status, error_reason, created_at, updated_at
		FROM transfers
		WHERE from_wallet_id = $1 OR to_wallet_id = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`
	rows, err := tx.QueryContext(ctx, query, walletID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to list transfers: %w", err)
	}
	defer rows.Close()

	var transfers []domain.Transfer
	for rows.Next() {
		var transfer domain.Transfer
		var errorReason sql.NullString
		err := rows.Scan(
			&transfer.ID,
			&transfer.FromWalletID,
			&transfer.ToWalletID,
			&transfer.Amount,
			&transfer.Status,
			&errorReason,
			&transfer.CreatedAt,
			&transfer.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan transfer: %w", err)
		}
		if errorReason.Valid {
			transfer.ErrorReason = &errorReason.String
		}
		transfers = append(transfers, transfer)
	}

	return transfers, rows.Err()
}
