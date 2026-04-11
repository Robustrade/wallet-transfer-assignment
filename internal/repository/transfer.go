package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"wallet-transfer/internal/domain"
)

type TransferRepository struct {
	db *sql.DB
}

func NewTransferRepository(db *sql.DB) *TransferRepository {
	return &TransferRepository{db: db}
}

func (r *TransferRepository) Create(ctx context.Context, exec Executor, t *domain.Transfer) error {
	var key *string
	if t.IdempotencyKey != "" {
		key = &t.IdempotencyKey
	}
	_, err := exec.ExecContext(ctx,
		`INSERT INTO transfers
		    (id, idempotency_key, from_wallet_id, to_wallet_id, amount, status, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		t.ID, key, t.FromWalletID, t.ToWalletID, t.Amount, string(t.Status), t.CreatedAt, t.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("create transfer: %w", err)
	}
	return nil
}

func (r *TransferRepository) GetByID(ctx context.Context, id string) (*domain.Transfer, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, idempotency_key, from_wallet_id, to_wallet_id, amount, status, created_at, updated_at
		 FROM transfers WHERE id = ?`,
		id,
	)
	return scanTransfer(row)
}

func (r *TransferRepository) GetByIdempotencyKey(ctx context.Context, key string) (*domain.Transfer, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, idempotency_key, from_wallet_id, to_wallet_id, amount, status, created_at, updated_at
		 FROM transfers WHERE idempotency_key = ?`,
		key,
	)
	return scanTransfer(row)
}

func (r *TransferRepository) UpdateStatus(ctx context.Context, exec Executor, id string, status domain.TransferStatus, updatedAt time.Time) error {
	_, err := exec.ExecContext(ctx,
		`UPDATE transfers SET status = ?, updated_at = ? WHERE id = ?`,
		string(status), updatedAt, id,
	)
	if err != nil {
		return fmt.Errorf("update transfer status: %w", err)
	}
	return nil
}

func scanTransfer(row *sql.Row) (*domain.Transfer, error) {
	t := &domain.Transfer{}
	var key sql.NullString
	err := row.Scan(
		&t.ID, &key, &t.FromWalletID, &t.ToWalletID,
		&t.Amount, &t.Status, &t.CreatedAt, &t.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrTransferNotFound
		}
		return nil, fmt.Errorf("scan transfer: %w", err)
	}
	if key.Valid {
		t.IdempotencyKey = key.String
	}
	return t, nil
}
