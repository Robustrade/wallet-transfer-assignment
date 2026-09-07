package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"Wallet_Transfer_Service/internal/domain"
)

type transferRepo struct {
	db dbtx
}

func (r *transferRepo) Create(ctx context.Context, t *domain.Transfer) error {
	const q = `
		INSERT INTO transfers
			(id, idempotency_key, from_wallet_id, to_wallet_id, amount, state, failure_reason, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`
	_, err := r.db.ExecContext(ctx, q,
		t.ID, t.IdempotencyKey, t.FromWalletID, t.ToWalletID, t.Amount,
		t.State, nullableString(t.FailureReason), t.CreatedAt, t.UpdatedAt)
	if err != nil {
		return fmt.Errorf("insert transfer: %w", err)
	}
	return nil
}

func (r *transferRepo) UpdateState(ctx context.Context, t *domain.Transfer) error {
	const q = `
		UPDATE transfers
		SET state = $1, failure_reason = $2, updated_at = $3
		WHERE id = $4`
	res, err := r.db.ExecContext(ctx, q, t.State, nullableString(t.FailureReason), t.UpdatedAt, t.ID)
	if err != nil {
		return fmt.Errorf("update transfer state: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("update transfer state: %w", err)
	}
	if n == 0 {
		return domain.ErrTransferNotFound
	}
	return nil
}

func (r *transferRepo) Get(ctx context.Context, id string) (*domain.Transfer, error) {
	const q = `
		SELECT id, idempotency_key, from_wallet_id, to_wallet_id, amount, state,
		       COALESCE(failure_reason, ''), created_at, updated_at
		FROM transfers WHERE id = $1`
	t := &domain.Transfer{}
	err := r.db.QueryRowContext(ctx, q, id).Scan(
		&t.ID, &t.IdempotencyKey, &t.FromWalletID, &t.ToWalletID, &t.Amount, &t.State,
		&t.FailureReason, &t.CreatedAt, &t.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrTransferNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get transfer: %w", err)
	}
	return t, nil
}

func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}
