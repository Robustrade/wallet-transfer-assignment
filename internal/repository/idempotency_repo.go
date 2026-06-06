package repository

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
)

// idempotencyRepo is the PostgreSQL implementation of IdempotencyRepository.
type idempotencyRepo struct {
	log *slog.Logger
}

// NewIdempotencyRepo returns an IdempotencyRepository backed by PostgreSQL.
func NewIdempotencyRepo(log *slog.Logger) IdempotencyRepository {
	return &idempotencyRepo{log: log}
}

func (r *idempotencyRepo) Get(ctx context.Context, q Querier, key string) (IdempotencyRecord, error) {
	const query = `
		SELECT key, transfer_id, response_json, status_code, request_hash
		FROM idempotency_records WHERE key = $1`

	var rec IdempotencyRecord

	err := q.QueryRow(ctx, query, key).Scan(
		&rec.Key, &rec.TransferID, &rec.ResponseJSON, &rec.StatusCode, &rec.RequestHash,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return IdempotencyRecord{}, domain.ErrIdempotencyKeyNotFound
		}

		r.log.Error("idempotency get failed", "layer", "repo", "error", err, "key", key)

		return IdempotencyRecord{}, fmt.Errorf("repo: get idempotency record: %w", err)
	}

	return rec, nil
}

func (r *idempotencyRepo) Create(ctx context.Context, tx pgx.Tx, rec IdempotencyRecord) error {
	const query = `
		INSERT INTO idempotency_records (key, transfer_id, response_json, status_code, request_hash)
		VALUES ($1, $2, $3, $4, $5)`

	if _, err := tx.Exec(ctx, query, rec.Key, rec.TransferID, rec.ResponseJSON, rec.StatusCode, rec.RequestHash); err != nil {
		r.log.Error("idempotency create failed", "layer", "repo", "error", err, "key", rec.Key)

		return fmt.Errorf("repo: create idempotency record: %w", err)
	}

	return nil
}
