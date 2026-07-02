package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/priiyank-xd/wallet-transfer-service/internal/domain"
)

const pgUniqueViolation = "23505"

type TransferRepository interface {
	FindByIdempotencyKey(ctx context.Context, key string) (*domain.Transfer, error)
	FindIdempotencyRecord(ctx context.Context, key string) (responseBody []byte, httpStatus int, err error)
	CreateTransfer(ctx context.Context, tx pgx.Tx, transfer domain.Transfer) error
	UpdateTransferStatus(ctx context.Context, tx pgx.Tx, id string, status domain.TransferStatus) error
	InsertLedgerEntries(ctx context.Context, tx pgx.Tx, entries []domain.LedgerEntry) error
	SaveIdempotencyRecord(ctx context.Context, tx pgx.Tx, key, transferID string, responseBody []byte, httpStatus int) error
}

func (r *Repository) FindByIdempotencyKey(ctx context.Context, key string) (*domain.Transfer, error) {
	query := `
		SELECT id, idempotency_key, from_wallet_id, to_wallet_id, amount, status, created_at, updated_at
		FROM transfers
		WHERE idempotency_key = $1
	`

	var t domain.Transfer
	err := r.pool.QueryRow(ctx, query, key).Scan(
		&t.ID, &t.IdempotencyKey, &t.FromWalletID, &t.ToWalletID,
		&t.Amount, &t.Status, &t.CreatedAt, &t.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	return &t, nil
}

func (r *Repository) FindIdempotencyRecord(ctx context.Context, key string) ([]byte, int, error) {
	query := `SELECT response_body, http_status FROM idempotency_records WHERE idempotency_key = $1`

	var body []byte
	var status int

	err := r.pool.QueryRow(ctx, query, key).Scan(&body, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, 0, ErrNotFound
	}
	if err != nil {
		return nil, 0, err
	}

	return body, status, nil
}

func (r *Repository) SaveIdempotencyRecord(
	ctx context.Context,
	tx pgx.Tx,
	key string,
	transferID string,
	responseBody []byte,
	httpStatus int,
) error {
	query := `
		INSERT INTO idempotency_records (idempotency_key, transfer_id, response_body, http_status)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (idempotency_key) DO NOTHING
	`

	_, err := tx.Exec(ctx, query, key, transferID, responseBody, httpStatus)
	return err
}

func (r *Repository) CreateTransfer(ctx context.Context, tx pgx.Tx, transfer domain.Transfer) error {
	query := `
		INSERT INTO transfers (
			id, idempotency_key, from_wallet_id, to_wallet_id, amount, status, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`

	_, err := tx.Exec(ctx, query,
		transfer.ID, transfer.IdempotencyKey, transfer.FromWalletID, transfer.ToWalletID,
		transfer.Amount, transfer.Status, transfer.CreatedAt, transfer.UpdatedAt,
	)
	if err == nil {
		return nil
	}

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation {
		return ErrDuplicateIdempotency
	}
	return err
}

func (r *Repository) UpdateTransferStatus(ctx context.Context, tx pgx.Tx, id string, status domain.TransferStatus) error {
	query := `UPDATE transfers SET status = $2, updated_at = NOW() WHERE id = $1`

	_, err := tx.Exec(ctx, query, id, status)
	return err
}

func (r *Repository) InsertLedgerEntries(ctx context.Context, tx pgx.Tx, entries []domain.LedgerEntry) error {
	query := `
		INSERT INTO ledger_entries (transfer_id, wallet_id, entry_type, amount)
		VALUES ($1, $2, $3, $4)
	`

	for _, e := range entries {
		if _, err := tx.Exec(ctx, query, e.TransferID, e.WalletID, e.EntryType, e.Amount); err != nil {
			return err
		}
	}

	return nil
}
