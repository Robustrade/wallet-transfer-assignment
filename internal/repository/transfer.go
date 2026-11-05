package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/Robustrade/wallet-transfer-assignment/internal/db"
	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
)

// TransferRepository persists transfers.
type TransferRepository struct{}

const transferColumns = `id, from_wallet_id, to_wallet_id, amount, status, failure_reason, created_at, updated_at`

func scanTransfer(row interface{ Scan(...any) error }) (domain.Transfer, error) {
	var (
		t      domain.Transfer
		reason sql.NullString
	)
	err := row.Scan(&t.ID, &t.FromWalletID, &t.ToWalletID, &t.Amount, &t.Status, &reason,
		&t.CreatedAt, &t.UpdatedAt)
	if reason.Valid {
		r := domain.FailureReason(reason.String)
		t.FailureReason = &r
	}
	return t, err
}

func oneTransfer(row *sql.Row, op string) (*domain.Transfer, error) {
	t, err := scanTransfer(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	return &t, nil
}

// InsertPending stores a new transfer in PENDING state.
func (TransferRepository) InsertPending(ctx context.Context, q db.Querier, id, from, to string, amount int64) (*domain.Transfer, error) {
	return oneTransfer(q.QueryRowContext(ctx,
		`INSERT INTO transfers (id, from_wallet_id, to_wallet_id, amount, status)
		 VALUES ($1, $2, $3, $4, 'PENDING')
		 RETURNING `+transferColumns, id, from, to, amount), "insert transfer")
}

// FindByID returns (nil, nil) if the transfer does not exist.
func (TransferRepository) FindByID(ctx context.Context, q db.Querier, id string) (*domain.Transfer, error) {
	return oneTransfer(q.QueryRowContext(ctx,
		`SELECT `+transferColumns+` FROM transfers WHERE id = $1`, id), "find transfer")
}

// LockByID takes a row lock on the transfer. Must be called inside a transaction.
func (TransferRepository) LockByID(ctx context.Context, q db.Querier, id string) (*domain.Transfer, error) {
	return oneTransfer(q.QueryRowContext(ctx,
		`SELECT `+transferColumns+` FROM transfers WHERE id = $1 FOR UPDATE`, id), "lock transfer")
}

// UpdateStatusIfPending is a compare-and-set: it only updates a transfer that is still
// PENDING and returns (nil, nil) otherwise, so a final state can never be overwritten.
func (TransferRepository) UpdateStatusIfPending(ctx context.Context, q db.Querier, id string,
	status domain.TransferStatus, reason *domain.FailureReason,
) (*domain.Transfer, error) {
	return oneTransfer(q.QueryRowContext(ctx,
		`UPDATE transfers
		    SET status = $2, failure_reason = $3, updated_at = now()
		  WHERE id = $1 AND status = 'PENDING'
		  RETURNING `+transferColumns, id, status, reason), "update transfer status")
}
