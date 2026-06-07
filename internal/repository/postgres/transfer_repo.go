package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
	"github.com/jackc/pgx/v5"
)

// transferRepo implements repository.TransferRepository against PostgreSQL.
type transferRepo struct {
	q querier
}

const transferColumns = "id, idempotency_key, from_wallet_id, to_wallet_id, amount, currency, status, failure_reason, created_at, updated_at"

// InsertIfAbsent inserts a PENDING transfer, relying on the unique constraint on
// idempotency_key for deduplication.
//
// INSERT ... ON CONFLICT DO NOTHING returns no row when the key already exists.
// In that case the existing transfer is fetched and returned so the caller can
// replay the original result. This is atomic and safe under concurrent
// duplicate requests: exactly one INSERT wins.
func (r *transferRepo) InsertIfAbsent(ctx context.Context, t *domain.Transfer) (bool, *domain.Transfer, error) {
	const insert = `
		INSERT INTO transfers (id, idempotency_key, from_wallet_id, to_wallet_id, amount, currency, status, failure_reason, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (idempotency_key) DO NOTHING
		RETURNING ` + transferColumns

	inserted, err := scanTransfer(r.q.QueryRow(ctx, insert,
		t.ID, t.IdempotencyKey, t.FromWalletID, t.ToWalletID, t.Amount, t.Currency, t.Status, t.FailureReason, t.CreatedAt, t.UpdatedAt))
	if err == nil {
		return true, inserted, nil
	}
	if !errors.Is(err, domain.ErrTransferNotFound) {
		return false, nil, fmt.Errorf("insert transfer: %w", err)
	}

	// Conflict: a transfer with this key already exists. Return it for replay.
	existing, getErr := r.getByIdempotencyKey(ctx, t.IdempotencyKey)
	if getErr != nil {
		return false, nil, fmt.Errorf("load existing transfer: %w", getErr)
	}
	return false, existing, nil
}

// Get reads a transfer by id.
func (r *transferRepo) Get(ctx context.Context, id string) (*domain.Transfer, error) {
	const query = "SELECT " + transferColumns + " FROM transfers WHERE id = $1"
	return scanTransfer(r.q.QueryRow(ctx, query, id))
}

// getByIdempotencyKey reads a transfer by its idempotency key.
func (r *transferRepo) getByIdempotencyKey(ctx context.Context, key string) (*domain.Transfer, error) {
	const query = "SELECT " + transferColumns + " FROM transfers WHERE idempotency_key = $1"
	return scanTransfer(r.q.QueryRow(ctx, query, key))
}

// UpdateStatus persists a status and failure-reason change.
func (r *transferRepo) UpdateStatus(ctx context.Context, t *domain.Transfer) error {
	const query = `
		UPDATE transfers
		SET status = $2, failure_reason = $3, currency = $4, updated_at = now()
		WHERE id = $1`
	tag, err := r.q.Exec(ctx, query, t.ID, t.Status, t.FailureReason, t.Currency)
	if err != nil {
		return fmt.Errorf("update transfer status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrTransferNotFound
	}
	return nil
}

// scanTransfer maps a single row into a domain.Transfer.
func scanTransfer(row pgx.Row) (*domain.Transfer, error) {
	var t domain.Transfer
	err := row.Scan(&t.ID, &t.IdempotencyKey, &t.FromWalletID, &t.ToWalletID,
		&t.Amount, &t.Currency, &t.Status, &t.FailureReason, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrTransferNotFound
		}
		return nil, fmt.Errorf("scan transfer: %w", err)
	}
	return &t, nil
}
