package repository

import (
	"context"
	"database/sql"

	"github.com/lib/pq"
	"github.com/robustrade/wallet-transfer-assignment/internal/model"
)

type transferRepository struct {
	db *sql.DB
}

func NewTransferRepository(db *sql.DB) TransferRepository {
	return &transferRepository{db: db}
}

func (r *transferRepository) WalletsExist(ctx context.Context, ids []string) (int, error) {
	query := `SELECT COUNT(*) FROM wallet_balances WHERE wallet_id = ANY($1)`

	var count int
	err := r.db.QueryRowContext(ctx, query, pq.Array(ids)).Scan(&count)
	if err != nil {
		return 0, err
	}

	return count, nil
}

func (r *transferRepository) InsertTransfer(ctx context.Context, tx *sql.Tx, transfer *model.Transfer) (err error, exists bool) {
	query := `
		INSERT INTO transfers (
			id,
			idempotency_key,
			from_wallet_id,
			to_wallet_id,
			amount,
			status,
			created_at,
			updated_at
		)
		VALUES ($1, $2, $3, $4, $5, 'PENDING', NOW(), NOW())
		ON CONFLICT (idempotency_key)
		DO NOTHING
		RETURNING id, status, created_at, updated_at;
	`
	row := tx.QueryRowContext(
		ctx,
		query,
		transfer.Id,
		transfer.IdempotencyKey,
		transfer.FromWalletId,
		transfer.ToWalletId,
		transfer.Amount,
	)

	err = row.Scan(&transfer.Id, &transfer.Status, &transfer.CreatedAt, &transfer.UpdatedAt)

	// Insert success
	if err == nil {
		return nil, false
	}

	// Conflict happened, fetch existing row
	if err == sql.ErrNoRows {
		query = `
			SELECT id, status, created_at, updated_at
			FROM transfers
			WHERE idempotency_key = $1
		`

		err = tx.QueryRowContext(ctx, query, transfer.IdempotencyKey).
			Scan(&transfer.Id, &transfer.Status, &transfer.CreatedAt, &transfer.UpdatedAt)
		if err != nil {
			return err, false
		}

		return nil, true
	}

	return err, false
}

func (r *transferRepository) InsertOutboxEntry(ctx context.Context, tx *sql.Tx, entry *model.OutboxEntry) error {
	query := `
		INSERT INTO outbox (
			id,
			transfer_id,
			event_type,
			transfer_status,
			event_status,
			payload,
			created_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, NOW())
	`
	_, err := tx.ExecContext(
		ctx,
		query,
		entry.Id,
		entry.TransferId,
		entry.EventType,
		entry.TransferStatus,
		entry.EventStatus,
		entry.Payload,
	)
	if err != nil {
		return err
	}
	return nil
}
