package repository

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
)

// transferRepo is the PostgreSQL implementation of TransferRepository.
type transferRepo struct {
	log *slog.Logger
}

// NewTransferRepo returns a TransferRepository backed by PostgreSQL.
func NewTransferRepo(log *slog.Logger) TransferRepository {
	return &transferRepo{log: log}
}

func (r *transferRepo) Create(ctx context.Context, q Querier, t domain.Transfer) error {
	const query = `
		INSERT INTO transfers
			(id, idempotency_key, from_wallet_id, to_wallet_id, amount, status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`

	if _, err := q.Exec(ctx, query,
		t.ID, t.IdempotencyKey, t.FromWalletID, t.ToWalletID,
		int64(t.Amount), int(t.Status), t.CreatedAt, t.UpdatedAt,
	); err != nil {
		r.log.Error("transfer create failed", "layer", "repo", "error", err, "transferID", t.ID)

		return fmt.Errorf("repo: create transfer: %w", err)
	}

	return nil
}

func (r *transferRepo) GetByID(ctx context.Context, q Querier, id uuid.UUID) (domain.Transfer, error) {
	const query = `
		SELECT id, idempotency_key, from_wallet_id, to_wallet_id, amount, status, created_at, updated_at
		FROM transfers WHERE id = $1`

	return r.scanTransfer(q.QueryRow(ctx, query, id))
}

func (r *transferRepo) UpdateStatus(ctx context.Context, tx pgx.Tx, id uuid.UUID, status domain.TransferStatus) error {
	const query = `UPDATE transfers SET status = $1, updated_at = NOW() WHERE id = $2`

	tag, err := tx.Exec(ctx, query, int(status), id)
	if err != nil {
		r.log.Error("transfer update status failed", "layer", "repo", "error", err, "transferID", id)

		return fmt.Errorf("repo: update transfer status: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return fmt.Errorf("repo: update transfer status: %w", domain.ErrTransferNotFound)
	}

	return nil
}

func (r *transferRepo) scanTransfer(row pgx.Row) (domain.Transfer, error) {
	var t domain.Transfer
	var amount int64
	var status int

	err := row.Scan(
		&t.ID, &t.IdempotencyKey, &t.FromWalletID, &t.ToWalletID,
		&amount, &status, &t.CreatedAt, &t.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Transfer{}, domain.ErrTransferNotFound
		}

		return domain.Transfer{}, fmt.Errorf("repo: scan transfer: %w", err)
	}

	t.Amount = domain.Amount(amount)
	t.Status = domain.TransferStatus(status)

	return t, nil
}
