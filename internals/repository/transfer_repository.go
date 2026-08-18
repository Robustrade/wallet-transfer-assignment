package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/AbhishekBadgujar/wallet-transfer-assignment/internals/models"
	"github.com/jackc/pgx/v5"
)

var ErrTransferNotFound = errors.New("transfer not found")

// TransferRepository handles transfer persistence.
type TransferRepository interface {
	Create(ctx context.Context, tx pgx.Tx, t *models.Transfer) error
	UpdateState(ctx context.Context, tx pgx.Tx, t *models.Transfer) error
	GetByIdempotencyKey(ctx context.Context, tx pgx.Tx, key string) (*models.Transfer, error)
}

type pgxTransferRepository struct{}

func NewTransferRepository() TransferRepository {
	return &pgxTransferRepository{}
}

// Create inserts a new transfer row. Relies on the DB-level unique index on
// idempotency_key to reject a second concurrent insert with the same key —
// callers should check for a unique_violation and treat it as "someone else
// is already processing this request."
func (r *pgxTransferRepository) Create(ctx context.Context, tx pgx.Tx, t *models.Transfer) error {
	// Used parameter placeholders so query is safe from SQL Injection attacks.
	const query = `
		INSERT INTO transfers (id, from_wallet_id, to_wallet_id, amount, state, idempotency_key)
		VALUES ($1, $2, $3, $4, $5, $6)`

	_, err := tx.Exec(ctx, query, t.ID, t.FromWalletID, t.ToWalletID, t.Amount, t.State, t.IdempotencyKey)
	if err != nil {
		return fmt.Errorf("inserting transfer: %w", err)
	}
	return nil
}

// UpdateState persists a transfer's state transition (and failure reason, if any).
func (r *pgxTransferRepository) UpdateState(ctx context.Context, tx pgx.Tx, t *models.Transfer) error {
	// Used parameter placeholders so query is safe from SQL Injection attacks.
	const query = `
		UPDATE transfers
		SET state = $1, failure_reason = $2, updated_at = now()
		WHERE id = $3`

	tag, err := tx.Exec(ctx, query, t.State, nullableString(t.FailureReason), t.ID)
	if err != nil {
		return fmt.Errorf("updating transfer %s state: %w", t.ID, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrTransferNotFound
	}
	return nil
}

// GetByIdempotencyKey looks up an existing transfer by its idempotency key,
// Used to detect and short-circuit duplicate requests.
func (r *pgxTransferRepository) GetByIdempotencyKey(ctx context.Context, tx pgx.Tx, key string) (*models.Transfer, error) {
	// Used parameter placeholders so query is safe from SQL Injection attacks.
	const query = `
		SELECT id, from_wallet_id, to_wallet_id, amount, state, COALESCE(failure_reason, ''), idempotency_key, created_at, updated_at
		FROM transfers
		WHERE idempotency_key = $1`

	var t models.Transfer
	err := tx.QueryRow(ctx, query, key).Scan(
		&t.ID, &t.FromWalletID, &t.ToWalletID, &t.Amount, &t.State,
		&t.FailureReason, &t.IdempotencyKey, &t.CreatedAt, &t.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil // not found is not an error here — caller decides what to do
		}
		return nil, fmt.Errorf("looking up transfer by idempotency key: %w", err)
	}
	return &t, nil
}

func nullableString(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}
