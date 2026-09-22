package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/wallet-transfer-assignment/wallet-transfer/internal/domain"
)

type transferRepo struct {
	db dbtx
}

func (r *transferRepo) Create(ctx context.Context, t *domain.Transfer) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO transfers (id, idempotency_key, from_wallet_id, to_wallet_id, amount, state, failure_reason, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, NULLIF($7, ''), $8, $8)`,
		t.ID, t.IdempotencyKey, t.FromWalletID, t.ToWalletID, t.Amount, t.State, t.FailureReason, t.CreatedAt)
	if isUniqueViolation(err) {
		return domain.ErrIdempotencyKeyConflict
	}
	return err
}

func (r *transferRepo) UpdateState(ctx context.Context, id string, state domain.TransferState, failureReason string) error {
	tag, err := r.db.Exec(ctx, `
		UPDATE transfers
		SET state = $2, failure_reason = NULLIF($3, ''), updated_at = now()
		WHERE id = $1`,
		id, state, failureReason)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrTransferNotFound
	}
	return nil
}

func (r *transferRepo) GetByID(ctx context.Context, id string) (*domain.Transfer, error) {
	row := r.db.QueryRow(ctx, `
		SELECT id, idempotency_key, from_wallet_id, to_wallet_id, amount, state, COALESCE(failure_reason, ''), created_at, updated_at
		FROM transfers WHERE id = $1`, id)
	return scanTransfer(row)
}

func (r *transferRepo) ListByWallet(ctx context.Context, walletID string) ([]domain.Transfer, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, idempotency_key, from_wallet_id, to_wallet_id, amount, state, COALESCE(failure_reason, ''), created_at, updated_at
		FROM transfers
		WHERE from_wallet_id = $1 OR to_wallet_id = $1
		ORDER BY created_at DESC`, walletID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var transfers []domain.Transfer
	for rows.Next() {
		t, err := scanTransfer(rows)
		if err != nil {
			return nil, err
		}
		transfers = append(transfers, *t)
	}
	return transfers, rows.Err()
}

// rowScanner is satisfied by both pgx.Row and pgx.Rows.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanTransfer(row rowScanner) (*domain.Transfer, error) {
	var t domain.Transfer
	err := row.Scan(&t.ID, &t.IdempotencyKey, &t.FromWalletID, &t.ToWalletID, &t.Amount, &t.State, &t.FailureReason, &t.CreatedAt, &t.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrTransferNotFound
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}
