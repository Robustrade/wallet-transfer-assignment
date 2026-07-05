package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/ravindranathreddy/wallet-transfer-assignment/internal/domain"
)

type IdempotencyRepository struct{}

func NewIdempotencyRepository() *IdempotencyRepository {
	return &IdempotencyRepository{}
}

func (r *IdempotencyRepository) InsertIdempotencyKey(ctx context.Context, db Executor, record domain.IdempotencyRecord) error {
	insertQuery := `INSERT INTO idempotency_records (idempotency_key, request_hash, transfer_id, created_at) VALUES ($1, $2, $3, $4)`
	_, err := db.Exec(ctx, insertQuery, record.IdempotencyKey, record.RequestHash, record.TransferID, record.CreatedAt)
	return err
}

func (r *IdempotencyRepository) GetIdempotencyRecord(ctx context.Context, db Executor, idempotencyKey string) (domain.IdempotencyRecord, error) {
	selectQuery := `SELECT idempotency_key, request_hash, transfer_id, created_at FROM idempotency_records WHERE idempotency_key = $1`
	var record domain.IdempotencyRecord
	err := db.QueryRow(ctx, selectQuery, idempotencyKey).Scan(&record.IdempotencyKey, &record.RequestHash, &record.TransferID, &record.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.IdempotencyRecord{}, domain.ErrIdempotencyRecordNotFound
		}
		return domain.IdempotencyRecord{}, err
	}
	return record, nil
}
