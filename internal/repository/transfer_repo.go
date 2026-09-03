package repository

import (
	"context"

	"github.com/rohitkumar27/wallet-transfer-assignment/internal/domain"
)

type TransferRepo struct{}

func NewTransferRepo() *TransferRepo {
	return &TransferRepo{}
}

func (r *TransferRepo) Create(ctx context.Context, q Querier, t *domain.Transfer) error {
	_, err := q.ExecContext(ctx, `
		INSERT INTO transfers (id, from_wallet_id, to_wallet_id, amount, status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, t.ID, t.FromWalletID, t.ToWalletID, t.Amount, t.Status, t.CreatedAt, t.UpdatedAt)
	return err
}

func (r *TransferRepo) UpdateStatus(ctx context.Context, q Querier, t *domain.Transfer) error {
	_, err := q.ExecContext(ctx, `
		UPDATE transfers SET status = $2, failure_reason = $3, updated_at = now() WHERE id = $1
	`, t.ID, t.Status, t.FailureReason)
	return err
}

func (r *TransferRepo) GetByID(ctx context.Context, q Querier, id string) (*domain.Transfer, error) {
	var t domain.Transfer
	err := q.QueryRowContext(ctx, `
		SELECT id, from_wallet_id, to_wallet_id, amount, status, failure_reason, created_at, updated_at
		FROM transfers WHERE id = $1
	`, id).Scan(&t.ID, &t.FromWalletID, &t.ToWalletID, &t.Amount, &t.Status, &t.FailureReason, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &t, nil
}
