package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"wallet/internal/constants"
	"wallet/internal/domain"
)

// CreateTransfer implements service.Tx.
func (t *txStore) CreateTransfer(ctx context.Context, fromWalletID, toWalletID string, amount int64) (domain.Transfer, error) {
	var tr domain.Transfer
	err := t.tx.QueryRow(ctx, constants.CreateTransferSQL, fromWalletID, toWalletID, amount).
		Scan(&tr.ID, &tr.FromWalletID, &tr.ToWalletID, &tr.Amount, &tr.Status,
			&tr.FailureReason, &tr.CreatedAt, &tr.UpdatedAt)

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23503" { // foreign_key_violation
		// The wallets were locked and checked moments ago in this same
		// transaction, so this can only fire if wallets are being deleted
		// concurrently (no such code path exists today).
		return domain.Transfer{}, fmt.Errorf("%w: %s", domain.ErrWalletNotFound, pgErr.Detail)
	}
	if err != nil {
		return domain.Transfer{}, mapError(fmt.Errorf("create transfer: %w", err))
	}
	return tr, nil
}

// MarkProcessed implements service.Tx.
func (t *txStore) MarkProcessed(ctx context.Context, transferID string) error {
	tag, err := t.tx.Exec(ctx, constants.MarkProcessedSQL, transferID)
	if err != nil {
		return mapError(fmt.Errorf("mark transfer processed: %w", err))
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("mark transfer processed: updated %d rows, want exactly 1", tag.RowsAffected())
	}
	return nil
}

// MarkFailed implements service.Tx.
func (t *txStore) MarkFailed(ctx context.Context, transferID, reason string) error {
	tag, err := t.tx.Exec(ctx, constants.MarkFailedSQL, transferID, reason)
	if err != nil {
		return mapError(fmt.Errorf("mark transfer failed: %w", err))
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("mark transfer failed: updated %d rows, want exactly 1", tag.RowsAffected())
	}
	return nil
}

// GetTransfer implements service.Store.
func (s *Store) GetTransfer(ctx context.Context, id string) (domain.Transfer, error) {
	ctx, cancel := boundedPoolCtx(ctx)
	defer cancel()

	var tr domain.Transfer
	err := s.pool.QueryRow(ctx, constants.GetTransferSQL, id).
		Scan(&tr.ID, &tr.FromWalletID, &tr.ToWalletID, &tr.Amount, &tr.Status,
			&tr.FailureReason, &tr.CreatedAt, &tr.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Transfer{}, fmt.Errorf("%w: %s", domain.ErrTransferNotFound, id)
	}
	if err != nil {
		return domain.Transfer{}, mapError(fmt.Errorf("get transfer: %w", err))
	}
	return tr, nil
}
