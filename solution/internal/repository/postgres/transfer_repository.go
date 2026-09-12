package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/heisenberglit/wallet-transfer-assignment/internal/domain"
)

// TransferRepository is the pgx-backed implementation of repository.TransferRepository.
type TransferRepository struct {
	pool *pgxpool.Pool
}

func NewTransferRepository(pool *pgxpool.Pool) *TransferRepository {
	return &TransferRepository{pool: pool}
}

// Create inserts the transfer row, mapping a unique-key violation to
// ErrIdempotencyConflict and a foreign-key violation to ErrWalletNotFound.
func (r *TransferRepository) Create(ctx context.Context, t *domain.Transfer) error {
	const query = `
		INSERT INTO transfers (id, idempotency_key, request_hash, from_wallet_id, to_wallet_id, amount, state, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`

	_, err := r.pool.Exec(ctx, query,
		t.ID, t.IdempotencyKey, t.RequestHash, t.FromWalletID, t.ToWalletID, t.Amount, t.State, t.CreatedAt, t.UpdatedAt)
	switch {
	case err == nil:
		return nil
	// Only the idempotency-key constraint means "someone else got here first".
	// A clash on the primary key is a genuine integrity error and must not be
	// dressed up as a 409.
	case isUniqueViolationOn(err, "transfers_idempotency_key_key"):
		return domain.ErrIdempotencyConflict
	case isForeignKeyViolation(err):
		return domain.ErrWalletNotFound
	default:
		return err
	}
}

// GetByIdempotencyKey returns (nil, nil), not an error, when key is unused.
func (r *TransferRepository) GetByIdempotencyKey(ctx context.Context, key string) (*domain.Transfer, error) {
	const query = `
		SELECT id, idempotency_key, request_hash, from_wallet_id, to_wallet_id, amount, state, created_at, updated_at
		FROM transfers WHERE idempotency_key = $1`

	var t domain.Transfer
	err := r.pool.QueryRow(ctx, query, key).Scan(
		&t.ID, &t.IdempotencyKey, &t.RequestHash, &t.FromWalletID, &t.ToWalletID, &t.Amount, &t.State, &t.CreatedAt, &t.UpdatedAt)
	switch {
	case err == nil:
		return &t, nil
	case errors.Is(err, pgx.ErrNoRows):
		return nil, nil
	default:
		return nil, err
	}
}

func (r *TransferRepository) UpdateState(ctx context.Context, id string, from, to domain.TransferState) error {
	if !from.CanTransitionTo(to) {
		return fmt.Errorf("%w: %s -> %s", domain.ErrInvalidStateTransition, from, to)
	}

	const query = `UPDATE transfers SET state = $3, updated_at = now() WHERE id = $1 AND state = $2`

	tag, err := r.pool.Exec(ctx, query, id, from, to)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrInvalidStateTransition
	}
	return nil
}
