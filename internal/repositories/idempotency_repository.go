package repositories

import (
	"database/sql"
	"fmt"
	"wallet-transfer-service/internal/models"

	"github.com/lib/pq"
)

type idempotencyRepository struct {
	db *sql.DB
}

// NewIdempotencyRepository creates a new idempotency repository
func NewIdempotencyRepository(db *sql.DB) IdempotencyRepository {
	return &idempotencyRepository{db: db}
}

// Create atomically creates an idempotency record
// Returns error if key already exists (duplicate request)
func (r *idempotencyRepository) Create(tx *sql.Tx, record *models.IdempotencyRecord) error {
	query := `INSERT INTO idempotency_records
	          (idempotency_key, request_hash, status, transfer_id, response_data, created_at, updated_at)
	          VALUES ($1, $2, $3, $4, $5, $6, $7)`

	// Convert empty response_data to NULL for JSONB field
	var responseData any
	if record.ResponseData != "" {
		responseData = record.ResponseData
	}

	_, err := tx.Exec(query,
		record.IdempotencyKey,
		record.RequestHash,
		record.Status,
		record.TransferID,
		responseData,
		record.CreatedAt,
		record.UpdatedAt,
	)

	if err != nil {
		// Check if it's a unique constraint violation
		if pqErr, ok := err.(*pq.Error); ok {
			if pqErr.Code == "23505" { // unique_violation
				return fmt.Errorf("duplicate idempotency key: %s", record.IdempotencyKey)
			}
		}
		return fmt.Errorf("failed to create idempotency record: %w", err)
	}

	return nil
}

// GetByKey retrieves an idempotency record by key
func (r *idempotencyRepository) GetByKey(tx *sql.Tx, key string) (*models.IdempotencyRecord, error) {
	query := `SELECT idempotency_key, request_hash, status, transfer_id, response_data, created_at, updated_at
	          FROM idempotency_records WHERE idempotency_key = $1`

	var record models.IdempotencyRecord
	var transferID sql.NullString
	var responseData sql.NullString

	err := tx.QueryRow(query, key).Scan(
		&record.IdempotencyKey,
		&record.RequestHash,
		&record.Status,
		&transferID,
		&responseData,
		&record.CreatedAt,
		&record.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, nil // Not found
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get idempotency record: %w", err)
	}

	// Handle nullable fields
	if transferID.Valid {
		record.TransferID = &transferID.String
	}
	if responseData.Valid {
		record.ResponseData = responseData.String
	}

	return &record, nil
}

// Update updates an existing idempotency record
func (r *idempotencyRepository) Update(tx *sql.Tx, record *models.IdempotencyRecord) error {
	query := `UPDATE idempotency_records
	          SET status = $1, transfer_id = $2, response_data = $3, updated_at = $4
	          WHERE idempotency_key = $5`

	result, err := tx.Exec(query,
		record.Status,
		record.TransferID,
		record.ResponseData,
		record.UpdatedAt,
		record.IdempotencyKey,
	)

	if err != nil {
		return fmt.Errorf("failed to update idempotency record: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("idempotency record not found: %s", record.IdempotencyKey)
	}

	return nil
}
