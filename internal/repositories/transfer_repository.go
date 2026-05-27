package repositories

import (
	"database/sql"
	"fmt"
	"wallet-transfer-service/internal/models"
)

type transferRepository struct {
	db *sql.DB
}

// NewTransferRepository creates a new transfer repository
func NewTransferRepository(db *sql.DB) TransferRepository {
	return &transferRepository{db: db}
}

// Create creates a new transfer
func (r *transferRepository) Create(tx *sql.Tx, transfer *models.Transfer) error {
	query := `INSERT INTO transfers (id, from_wallet_id, to_wallet_id, amount, status, created_at, updated_at)
	          VALUES ($1, $2, $3, $4, $5, $6, $7)`

	_, err := tx.Exec(query,
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
func (r *transferRepository) GetByID(tx *sql.Tx, id string) (*models.Transfer, error) {
	query := `SELECT id, from_wallet_id, to_wallet_id, amount, status, created_at, updated_at
	          FROM transfers WHERE id = $1`

	var transfer models.Transfer
	err := tx.QueryRow(query, id).Scan(
		&transfer.ID,
		&transfer.FromWalletID,
		&transfer.ToWalletID,
		&transfer.Amount,
		&transfer.Status,
		&transfer.CreatedAt,
		&transfer.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("transfer not found: %s", id)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get transfer: %w", err)
	}

	return &transfer, nil
}

// UpdateStatus updates the transfer status
func (r *transferRepository) UpdateStatus(tx *sql.Tx, id string, status models.TransferStatus) error {
	query := `UPDATE transfers SET status = $1, updated_at = CURRENT_TIMESTAMP WHERE id = $2`

	result, err := tx.Exec(query, status, id)
	if err != nil {
		return fmt.Errorf("failed to update transfer status: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("transfer not found: %s", id)
	}

	return nil
}
