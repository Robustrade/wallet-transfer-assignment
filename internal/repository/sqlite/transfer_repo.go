package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	driver "modernc.org/sqlite"

	"wallet-transfer-assignment/internal/domain"
	"wallet-transfer-assignment/internal/repository"
)

// sqliteConstraintUnique is SQLITE_CONSTRAINT_UNIQUE from sqlite3.h (19 |
// (8<<8)). modernc.org/sqlite surfaces it via (*sqlite.Error).Code().
const sqliteConstraintUnique = 2067

type transferRepo struct {
	tx *sql.Tx
}

func (r *transferRepo) Insert(ctx context.Context, t *domain.Transfer) error {
	_, err := r.tx.ExecContext(ctx, `
		INSERT INTO transfers
			(id, idempotency_key, from_wallet_id, to_wallet_id, amount, status, failure_reason, request_hash, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.ID, t.IdempotencyKey, t.FromWalletID, t.ToWalletID, t.Amount, t.Status, nullIfEmpty(t.FailureReason), t.RequestHash, t.CreatedAt, t.UpdatedAt)
	if err != nil {
		var sqliteErr *driver.Error
		if errors.As(err, &sqliteErr) && sqliteErr.Code() == sqliteConstraintUnique {
			return repository.ErrIdempotencyKeyExists
		}
		return fmt.Errorf("insert transfer: %w", err)
	}
	return nil
}

func (r *transferRepo) GetByIdempotencyKey(ctx context.Context, key string) (*domain.Transfer, error) {
	return r.scanOne(ctx, `
		SELECT id, idempotency_key, from_wallet_id, to_wallet_id, amount, status, COALESCE(failure_reason, ''), request_hash, created_at, updated_at
		FROM transfers WHERE idempotency_key = ?`, key)
}

func (r *transferRepo) GetByID(ctx context.Context, id string) (*domain.Transfer, error) {
	return r.scanOne(ctx, `
		SELECT id, idempotency_key, from_wallet_id, to_wallet_id, amount, status, COALESCE(failure_reason, ''), request_hash, created_at, updated_at
		FROM transfers WHERE id = ?`, id)
}

func (r *transferRepo) UpdateStatus(ctx context.Context, t *domain.Transfer) error {
	_, err := r.tx.ExecContext(ctx,
		`UPDATE transfers SET status = ?, failure_reason = ?, updated_at = ? WHERE id = ?`,
		t.Status, nullIfEmpty(t.FailureReason), t.UpdatedAt, t.ID)
	if err != nil {
		return fmt.Errorf("update transfer status: %w", err)
	}
	return nil
}

func (r *transferRepo) scanOne(ctx context.Context, query string, args ...any) (*domain.Transfer, error) {
	row := r.tx.QueryRowContext(ctx, query, args...)
	var t domain.Transfer
	if err := row.Scan(&t.ID, &t.IdempotencyKey, &t.FromWalletID, &t.ToWalletID, &t.Amount, &t.Status, &t.FailureReason, &t.RequestHash, &t.CreatedAt, &t.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, sql.ErrNoRows
		}
		return nil, fmt.Errorf("scan transfer: %w", err)
	}
	return &t, nil
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
