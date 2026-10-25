package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/GaureshPatil/wallet-transfer-assignment/internal/domain"
)

type transferRepo struct{ db dbtx }

const transferColumns = `id, idempotency_key, from_wallet_id, to_wallet_id, amount, status,
	COALESCE(failure_reason, ''), created_at, updated_at`

func (r *transferRepo) Insert(ctx context.Context, t *domain.Transfer) error {
	_, err := r.db.Exec(ctx,
		`INSERT INTO transfers (id, idempotency_key, from_wallet_id, to_wallet_id, amount, status, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		t.ID, t.IdempotencyKey, t.FromWalletID, t.ToWalletID, t.Amount, string(t.Status), t.CreatedAt, t.UpdatedAt)
	if err != nil {
		return fmt.Errorf("insert transfer: %w", err)
	}
	return nil
}

func (r *transferRepo) Get(ctx context.Context, id uuid.UUID) (*domain.Transfer, error) {
	var (
		t      domain.Transfer
		status string
		reason string
	)
	err := r.db.QueryRow(ctx, `SELECT `+transferColumns+` FROM transfers WHERE id = $1`, id).
		Scan(&t.ID, &t.IdempotencyKey, &t.FromWalletID, &t.ToWalletID, &t.Amount, &status, &reason, &t.CreatedAt, &t.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrTransferNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get transfer: %w", err)
	}
	t.Status = domain.TransferStatus(status)
	t.FailureReason = domain.FailureReason(reason)
	t.CreatedAt, t.UpdatedAt = t.CreatedAt.UTC(), t.UpdatedAt.UTC()
	return &t, nil
}

// SaveTransition is a compare-and-set on status: the WHERE clause makes a
// duplicate or concurrent state change a no-op that we surface as an error.
func (r *transferRepo) SaveTransition(ctx context.Context, t *domain.Transfer, from domain.TransferStatus) error {
	var reason *string
	if t.FailureReason != "" {
		s := string(t.FailureReason)
		reason = &s
	}
	tag, err := r.db.Exec(ctx,
		`UPDATE transfers SET status = $3, failure_reason = $4, updated_at = $5
		  WHERE id = $1 AND status = $2`,
		t.ID, string(from), string(t.Status), reason, t.UpdatedAt)
	if err != nil {
		return fmt.Errorf("update transfer status: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("%w: transfer %s is no longer %s", domain.ErrInvalidTransition, t.ID, from)
	}
	return nil
}
