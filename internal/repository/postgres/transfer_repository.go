package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/karthikadarvin/wallet-transfer-assignment/internal/domain"
	"github.com/karthikadarvin/wallet-transfer-assignment/internal/repository"
)

type TransferRepository struct{}

func NewTransferRepository() *TransferRepository {
	return &TransferRepository{}
}

func (r *TransferRepository) Create(ctx context.Context, q repository.Querier, t *domain.Transfer) error {
	row := q.QueryRowContext(ctx, `
		INSERT INTO transfers (from_wallet_id, to_wallet_id, amount, status)
		VALUES ($1, $2, $3, $4)
		RETURNING id, created_at, updated_at
	`, t.FromWalletID, t.ToWalletID, t.Amount, t.Status)

	if err := row.Scan(&t.ID, &t.CreatedAt, &t.UpdatedAt); err != nil {
		return fmt.Errorf("insert transfer: %w", err)
	}

	return nil
}

func (r *TransferRepository) UpdateStatus(ctx context.Context, q repository.Querier, id string, status domain.TransferStatus, failureReason string) error {
	var reason sql.NullString
	if failureReason != "" {
		reason = sql.NullString{String: failureReason, Valid: true}
	}

	res, err := q.ExecContext(ctx, `
		UPDATE transfers
		SET status = $1, failure_reason = $2, updated_at = now()
		WHERE id = $3
	`, status, reason, id)
	if err != nil {
		return fmt.Errorf("update transfer status: %w", err)
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if rows == 0 {
		return domain.ErrTransferNotFound
	}

	return nil
}

func (r *TransferRepository) GetByID(ctx context.Context, q repository.Querier, id string) (*domain.Transfer, error) {
	row := q.QueryRowContext(ctx, `
		SELECT id, from_wallet_id, to_wallet_id, amount, status,
		       COALESCE(failure_reason, ''), created_at, updated_at
		FROM transfers
		WHERE id = $1
	`, id)

	var t domain.Transfer
	if err := row.Scan(&t.ID, &t.FromWalletID, &t.ToWalletID, &t.Amount, &t.Status,
		&t.FailureReason, &t.CreatedAt, &t.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrTransferNotFound
		}
		return nil, fmt.Errorf("scan transfer: %w", err)
	}

	return &t, nil
}

// ListByWallet returns transfers where the wallet is either the source or
// destination, most recent first, paginated.
func (r *TransferRepository) ListByWallet(ctx context.Context, q repository.Querier, walletID string, limit, offset int) ([]*domain.Transfer, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT id, from_wallet_id, to_wallet_id, amount, status,
		       COALESCE(failure_reason, ''), created_at, updated_at
		FROM transfers
		WHERE from_wallet_id = $1 OR to_wallet_id = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`, walletID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("query transfers by wallet: %w", err)
	}
	defer rows.Close()

	var transfers []*domain.Transfer
	for rows.Next() {
		var t domain.Transfer
		if err := rows.Scan(&t.ID, &t.FromWalletID, &t.ToWalletID, &t.Amount, &t.Status,
			&t.FailureReason, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan transfer: %w", err)
		}
		transfers = append(transfers, &t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate transfers: %w", err)
	}

	return transfers, nil
}
