package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/PratikDhanave/wallet-transfer-assignment/internal/domain"
	"github.com/PratikDhanave/wallet-transfer-assignment/internal/repository"
)

// TransferRepo is the PostgreSQL implementation of
// repository.TransferRepository. Stateless; takes an Executor per call.
type TransferRepo struct{}

func NewTransferRepo() *TransferRepo { return &TransferRepo{} }

// Get returns the transfer for id, or repository.ErrNotFound.
//
// failure_reason is a nullable TEXT column at the schema level;
// COALESCE(..., ”) maps SQL NULL onto the Go zero value so callers
// always get a string rather than dealing with sql.NullString.
func (r *TransferRepo) Get(ctx context.Context, exec repository.Executor, id uuid.UUID) (*domain.Transfer, error) {
	const q = `
		SELECT id, from_wallet_id, to_wallet_id, amount, state,
		       COALESCE(failure_reason, ''), created_at, updated_at
		FROM transfers
		WHERE id = $1`
	var t domain.Transfer
	err := exec.QueryRowContext(ctx, q, id).Scan(
		&t.ID, &t.FromWalletID, &t.ToWalletID, &t.Amount, &t.State,
		&t.FailureReason, &t.CreatedAt, &t.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, repository.ErrNotFound
		}
		return nil, fmt.Errorf("scan transfer: %w", err)
	}
	return &t, nil
}

// Insert creates a new transfer row in the state the caller has set
// on t (in the standard flow that's StatePending; the service then
// drives the row to a terminal state inside the same transaction).
//
// RETURNING is used so the database-assigned timestamps land back on
// the struct without a second round trip.
func (r *TransferRepo) Insert(ctx context.Context, exec repository.Executor, t *domain.Transfer) error {
	const q = `
		INSERT INTO transfers (id, from_wallet_id, to_wallet_id, amount, state)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING created_at, updated_at`
	row := exec.QueryRowContext(ctx, q, t.ID, t.FromWalletID, t.ToWalletID, t.Amount, t.State)
	if err := row.Scan(&t.CreatedAt, &t.UpdatedAt); err != nil {
		return fmt.Errorf("insert transfer: %w", err)
	}
	return nil
}

// UpdateState moves the transfer to a new state.
//
// failureReason is mapped to NULL when empty (via NULLIF) so the
// schema's nullable TEXT column reflects "no reason" as SQL NULL
// rather than the literal string "". The state machine is enforced
// upstream by domain.TransferState.CanTransitionTo plus the schema
// CHECK constraint on the state column.
//
// Returns repository.ErrNotFound when no row matches the id.
func (r *TransferRepo) UpdateState(ctx context.Context, exec repository.Executor, id uuid.UUID, state domain.TransferState, failureReason string) error {
	const q = `
		UPDATE transfers
		SET state = $2, failure_reason = NULLIF($3, ''), updated_at = now()
		WHERE id = $1`
	res, err := exec.ExecContext(ctx, q, id, state, failureReason)
	if err != nil {
		return fmt.Errorf("update transfer state: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return repository.ErrNotFound
	}
	return nil
}

// ListByWallet returns transfers touching walletID (as source or
// destination) ordered by created_at DESC, id DESC. Pagination is
// cursor-based on (created_at, id): if `before` is non-nil, only
// rows STRICTLY older than that timestamp are returned. Limit is
// passed through as-is — the service layer is responsible for
// applying a sensible cap.
//
// The query uses the composite indexes on (from_wallet_id,
// created_at DESC, id DESC) and (to_wallet_id, created_at DESC,
// id DESC) added by migration 0002. Postgres BitmapOr-merges the
// two halves so even very large transfer tables stay fast.
func (r *TransferRepo) ListByWallet(ctx context.Context, exec repository.Executor, walletID string, limit int, before *time.Time) ([]domain.Transfer, error) {
	const q = `
		SELECT id, from_wallet_id, to_wallet_id, amount, state,
		       COALESCE(failure_reason, ''), created_at, updated_at
		FROM transfers
		WHERE (from_wallet_id = $1 OR to_wallet_id = $1)
		  AND ($2::timestamptz IS NULL OR created_at < $2)
		ORDER BY created_at DESC, id DESC
		LIMIT $3`
	rows, err := exec.QueryContext(ctx, q, walletID, before, limit)
	if err != nil {
		return nil, fmt.Errorf("list transfers by wallet: %w", err)
	}
	defer rows.Close()

	var out []domain.Transfer
	for rows.Next() {
		var t domain.Transfer
		if err := rows.Scan(
			&t.ID, &t.FromWalletID, &t.ToWalletID, &t.Amount, &t.State,
			&t.FailureReason, &t.CreatedAt, &t.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan transfer: %w", err)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
