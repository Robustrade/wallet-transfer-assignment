package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/candidate/wallet-transfer/internal/db"
	"github.com/candidate/wallet-transfer/internal/domain"
	"github.com/shopspring/decimal"
)

type TransactionRepository struct {
	db *db.DB
}

func NewTransactionRepository(d *db.DB) *TransactionRepository {
	return &TransactionRepository{db: d}
}

func (r *TransactionRepository) Create(ctx context.Context, tx *sql.Tx, t *domain.Transaction) (*domain.Transaction, error) {
	const q = `
		INSERT INTO transactions
			(idempotency_key, reference_id, from_wallet_id, to_wallet_id, amount, currency, status, description, metadata)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`

	res, err := tx.ExecContext(ctx, q,
		t.IdempotencyKey,
		t.ReferenceID,
		t.FromWalletID,
		t.ToWalletID,
		t.Amount.String(),
		t.Currency,
		t.Status,
		t.Description,
		t.Metadata,
	)
	if err != nil {
		if isDuplicateKeyError(err) {
			return nil, domain.ErrDuplicateIdempotencyKey
		}
		return nil, fmt.Errorf("transaction create: %w", err)
	}

	id, _ := res.LastInsertId()
	t.ID = uint64(id)

	// Re-fetch the inserted row to populate database-generated created_at/updated_at
	refetched, err := scanTransaction(tx.QueryRowContext(ctx,
		`SELECT id, idempotency_key, reference_id, from_wallet_id, to_wallet_id,
		        amount, currency, status, COALESCE(description,''), COALESCE(metadata,''), created_at, updated_at
		 FROM   transactions WHERE id = ?`, t.ID))
	if err != nil {
		return t, nil // fallback to in-memory object if re-fetch fails
	}
	return refetched, nil
}

func (r *TransactionRepository) GetByID(ctx context.Context, q db.Querier, id uint64) (*domain.Transaction, error) {
	const query = `
		SELECT id, idempotency_key, reference_id, from_wallet_id, to_wallet_id,
		       amount, currency, status, COALESCE(description,''), COALESCE(metadata,''), created_at, updated_at
		FROM   transactions
		WHERE  id = ?`

	return scanTransaction(q.QueryRowContext(ctx, query, id))
}

func (r *TransactionRepository) GetByIdempotencyKey(ctx context.Context, q db.Querier, key string) (*domain.Transaction, error) {
	const query = `
		SELECT id, idempotency_key, reference_id, from_wallet_id, to_wallet_id,
		       amount, currency, status, COALESCE(description,''), COALESCE(metadata,''), created_at, updated_at
		FROM   transactions
		WHERE  idempotency_key = ?`

	return scanTransaction(q.QueryRowContext(ctx, query, key))
}

func (r *TransactionRepository) UpdateStatus(ctx context.Context, tx *sql.Tx, id uint64, status domain.TransactionStatus) error {
	const q = `UPDATE transactions SET status = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`
	_, err := tx.ExecContext(ctx, q, status, id)
	return err
}

func scanTransaction(row *sql.Row) (*domain.Transaction, error) {
	var t domain.Transaction
	var amount string

	err := row.Scan(
		&t.ID,
		&t.IdempotencyKey,
		&t.ReferenceID,
		&t.FromWalletID,
		&t.ToWalletID,
		&amount,
		&t.Currency,
		&t.Status,
		&t.Description,
		&t.Metadata,
		&t.CreatedAt,
		&t.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrTransactionNotFound
		}
		return nil, fmt.Errorf("scan transaction: %w", err)
	}

	t.Amount, err = decimal.NewFromString(amount)
	if err != nil {
		return nil, fmt.Errorf("parse amount: %w", err)
	}
	return &t, nil
}

func isDuplicateKeyError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	// Only treat idempotency_key or reference_id UNIQUE violations as duplicates.
	// Do NOT conflate CHECK or foreign key constraint failures.
	if strings.Contains(msg, "UNIQUE constraint failed: transactions.idempotency_key") {
		return true
	}
	if strings.Contains(msg, "UNIQUE constraint failed: transactions.reference_id") {
		return true
	}
	// MySQL error code 1062 for duplicate entry
	if strings.Contains(msg, "1062") {
		return true
	}
	return false
}
