package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
)

type transferRepo struct{ q querier }

func (r transferRepo) Create(ctx context.Context, t domain.Transfer) error {
	_, err := r.q.Exec(ctx, `
		INSERT INTO transfers (id, from_wallet_id, to_wallet_id, amount, status, failure_reason, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''), $7, $8)`,
		t.ID, t.FromWalletID, t.ToWalletID, t.Amount, string(t.Status), string(t.FailureReason), t.CreatedAt, t.UpdatedAt)
	if err != nil {
		return translate(fmt.Errorf("insert transfer %s: %w", t.ID, err))
	}
	return nil
}

func (r transferRepo) UpdateStatus(ctx context.Context, t domain.Transfer, from domain.TransferStatus) error {
	// Guarded write: only succeeds if nobody moved the transfer out of `from` first.
	tag, err := r.q.Exec(ctx, `
		UPDATE transfers SET status = $2, failure_reason = NULLIF($3, ''), updated_at = $4
		WHERE id = $1 AND status = $5`,
		t.ID, string(t.Status), string(t.FailureReason), t.UpdatedAt, string(from))
	if err != nil {
		return translate(fmt.Errorf("update transfer %s: %w", t.ID, err))
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("update transfer %s from %s to %s: %w", t.ID, from, t.Status, domain.ErrInvalidTransition)
	}
	return nil
}

func (r transferRepo) Get(ctx context.Context, id uuid.UUID) (domain.Transfer, error) {
	var (
		t       domain.Transfer
		status  string
		reason  *string
		idemKey *string
	)
	err := r.q.QueryRow(ctx, `
		SELECT t.id, i.key, t.from_wallet_id, t.to_wallet_id, t.amount, t.status, t.failure_reason,
		       t.created_at, t.updated_at
		FROM transfers t
		LEFT JOIN idempotency_records i ON i.transfer_id = t.id
		WHERE t.id = $1`, id,
	).Scan(&t.ID, &idemKey, &t.FromWalletID, &t.ToWalletID, &t.Amount, &status, &reason, &t.CreatedAt, &t.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Transfer{}, fmt.Errorf("transfer %s: %w", id, domain.ErrTransferNotFound)
	}
	if err != nil {
		return domain.Transfer{}, translate(fmt.Errorf("load transfer %s: %w", id, err))
	}
	t.Status = domain.TransferStatus(status)
	if reason != nil {
		t.FailureReason = domain.FailureReason(*reason)
	}
	if idemKey != nil {
		t.IdempotencyKey = *idemKey
	}
	return t, nil
}
