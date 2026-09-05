package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"wallet-transfer/internal/domain"
)

type TransferRepository struct {
	tx *TxManager
}

func NewTransferRepository(tx *TxManager) *TransferRepository { return &TransferRepository{tx: tx} }

const transferColumns = `id, from_wallet_id, to_wallet_id, amount, currency, status, failure_reason, created_at, updated_at`

func (r *TransferRepository) Create(ctx context.Context, t *domain.Transfer) error {
	const query = `
		INSERT INTO transfers (id, from_wallet_id, to_wallet_id, amount, currency, status, failure_reason)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING created_at, updated_at`

	err := r.tx.executorFrom(ctx).QueryRowContext(ctx, query,
		t.ID, t.FromWalletID, t.ToWalletID, int64(t.Amount), t.Currency, string(t.Status), t.FailureReason,
	).Scan(&t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return fmt.Errorf("create transfer %s: %w", t.ID, translateError(err))
	}
	return nil
}

func (r *TransferRepository) UpdateStatus(ctx context.Context, id uuid.UUID, from, to domain.TransferStatus, reason *string) error {
	const query = `
		UPDATE transfers
		SET status = $3, failure_reason = $4, updated_at = now()
		WHERE id = $1 AND status = $2`

	result, err := r.tx.executorFrom(ctx).ExecContext(ctx, query, id, string(from), string(to), reason)
	if err != nil {
		return fmt.Errorf("update transfer %s status: %w", id, translateError(err))
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("update transfer %s status: %w", id, translateError(err))
	}
	if affected != 1 {
		return fmt.Errorf("%w: transfer %s is no longer %s", domain.ErrInvalidStateTransition, id, from)
	}
	return nil
}

func (r *TransferRepository) Get(ctx context.Context, id uuid.UUID) (domain.Transfer, error) {
	query := `SELECT ` + transferColumns + ` FROM transfers WHERE id = $1`

	t, err := scanTransfer(r.tx.executorFrom(ctx).QueryRowContext(ctx, query, id))
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return domain.Transfer{}, fmt.Errorf("%w: %s", domain.ErrTransferNotFound, id)
	case err != nil:
		return domain.Transfer{}, fmt.Errorf("get transfer %s: %w", id, translateError(err))
	}
	return t, nil
}

func (r *TransferRepository) ListByWallet(ctx context.Context, walletID string, limit int) ([]domain.Transfer, error) {
	query := `
		SELECT ` + transferColumns + `
		FROM transfers
		WHERE (from_wallet_id = $1 OR to_wallet_id = $1)
		ORDER BY created_at DESC, id DESC
		LIMIT $2`

	rows, err := r.tx.executorFrom(ctx).QueryContext(ctx, query, walletID, limit)
	if err != nil {
		return nil, fmt.Errorf("list transfers for %s: %w", walletID, translateError(err))
	}
	defer func() { _ = rows.Close() }()

	var transfers []domain.Transfer
	for rows.Next() {
		t, err := scanTransfer(rows)
		if err != nil {
			return nil, fmt.Errorf("scan transfer: %w", translateError(err))
		}
		transfers = append(transfers, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list transfers for %s: %w", walletID, translateError(err))
	}

	return transfers, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanTransfer(row rowScanner) (domain.Transfer, error) {
	var (
		t      domain.Transfer
		status string
	)
	err := row.Scan(&t.ID, &t.FromWalletID, &t.ToWalletID, &t.Amount, &t.Currency,
		&status, &t.FailureReason, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return domain.Transfer{}, err
	}
	t.Status = domain.TransferStatus(status)
	return t, nil
}
