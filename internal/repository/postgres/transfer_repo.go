package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/jaiminbhaduri/wallet-transfer-assignment/internal/domain"
)

type TransferRepo struct {
	db DBTX
}

func NewTransferRepo(db DBTX) *TransferRepo {
	return &TransferRepo{db: db}
}

// InsertPending attempts to create a new PENDING transfer for the given
// idempotency key. If a transfer with that key already exists, no row is
// inserted and created=false is returned — the caller should then fetch and
// return the existing transfer instead of re-executing the transfer.
//
// The uniqueness constraint on idempotency_key is the single source of truth
// for deduplication: this INSERT ... ON CONFLICT DO NOTHING is atomic, so it
// is safe even when two requests with the same key arrive concurrently.
func (r *TransferRepo) InsertPending(ctx context.Context, t domain.Transfer) (created bool, _ *domain.Transfer, _ error) {
	row := r.db.QueryRow(ctx, `
		INSERT INTO transfers (id, idempotency_key, from_wallet_id, to_wallet_id, amount, status)
		VALUES ($1, $2, $3, $4, $5, 'PENDING')
		ON CONFLICT (idempotency_key) DO NOTHING
		RETURNING id, idempotency_key, from_wallet_id, to_wallet_id, amount, status, failure_reason, created_at, updated_at
	`, t.ID, t.IdempotencyKey, t.FromWalletID, t.ToWalletID, t.Amount)

	inserted, err := scanTransfer(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil, nil
		}
		return false, nil, err
	}
	return true, inserted, nil
}

func (r *TransferRepo) GetByIdempotencyKey(ctx context.Context, key string) (*domain.Transfer, error) {
	row := r.db.QueryRow(ctx, `
		SELECT id, idempotency_key, from_wallet_id, to_wallet_id, amount, status, failure_reason, created_at, updated_at
		FROM transfers
		WHERE idempotency_key = $1
	`, key)
	return scanTransfer(row)
}

func (r *TransferRepo) GetByID(ctx context.Context, id string) (*domain.Transfer, error) {
	row := r.db.QueryRow(ctx, `
		SELECT id, idempotency_key, from_wallet_id, to_wallet_id, amount, status, failure_reason, created_at, updated_at
		FROM transfers
		WHERE id = $1
	`, id)
	return scanTransfer(row)
}

func (r *TransferRepo) MarkProcessed(ctx context.Context, id string) error {
	_, err := r.db.Exec(ctx, `
		UPDATE transfers
		SET status = 'PROCESSED', updated_at = now()
		WHERE id = $1
	`, id)
	return err
}

func (r *TransferRepo) MarkFailed(ctx context.Context, id string, reason string) error {
	_, err := r.db.Exec(ctx, `
		UPDATE transfers
		SET status = 'FAILED', failure_reason = $2, updated_at = now()
		WHERE id = $1
	`, id, reason)
	return err
}

func scanTransfer(row pgx.Row) (*domain.Transfer, error) {
	var t domain.Transfer
	if err := row.Scan(
		&t.ID, &t.IdempotencyKey, &t.FromWalletID, &t.ToWalletID, &t.Amount,
		&t.Status, &t.FailureReason, &t.CreatedAt, &t.UpdatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, pgx.ErrNoRows
		}
		return nil, err
	}
	return &t, nil
}
