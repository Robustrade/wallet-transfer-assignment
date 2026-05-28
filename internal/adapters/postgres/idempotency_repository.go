package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"wallet-transfer-assignment/internal/idempotency"
)

type IdempotencyRepository struct {
	pool *pgxpool.Pool
}

func NewIdempotencyRepository(pool *pgxpool.Pool) *IdempotencyRepository {
	return &IdempotencyRepository{pool: pool}
}

func (r *IdempotencyRepository) TryInsert(ctx context.Context, tx idempotency.Tx, key idempotency.Key, requestHash string) (bool, idempotency.Record, error) {
	var (
		inserted     bool
		existingHash string
		code         *int
		body         *[]byte
	)

	// Use ON CONFLICT DO UPDATE to ensure the conflicting row is locked and we can RETURNING the row
	// (this avoids races where the row exists but isn't yet visible/finalized to this transaction).
	err := tx.QueryRow(ctx,
		`INSERT INTO idempotency_records (idempotency_key, request_hash, response_code, response_body)
         VALUES ($1, $2, NULL, NULL)
         ON CONFLICT (idempotency_key) DO UPDATE
           SET request_hash = idempotency_records.request_hash,
               updated_at = now()
         RETURNING (xmax = 0) AS inserted, request_hash, response_code, response_body`,
		string(key), requestHash,
	).Scan(&inserted, &existingHash, &code, &body)
	if err != nil {
		return false, idempotency.Record{}, err
	}

	if inserted {
		return true, idempotency.Record{}, nil
	}

	ready := code != nil && body != nil
	rec := idempotency.Record{
		Key:         key,
		RequestHash: existingHash,
		Ready:       ready,
	}
	if ready {
		rec.StatusCode = *code
		rec.Body = json.RawMessage(*body)
	}
	return false, rec, nil
}

func (r *IdempotencyRepository) Get(ctx context.Context, key idempotency.Key) (idempotency.Record, error) {
	var (
		requestHash  string
		code         *int
		bodyNullable *[]byte
	)

	err := r.pool.QueryRow(ctx,
		`SELECT request_hash, response_code, response_body
         FROM idempotency_records
         WHERE idempotency_key = $1`,
		string(key),
	).Scan(&requestHash, &code, &bodyNullable)
	if isNoRows(err) {
		return idempotency.Record{}, fmt.Errorf("idempotency key not found")
	}
	if err != nil {
		return idempotency.Record{}, err
	}

	ready := code != nil && bodyNullable != nil

	rec := idempotency.Record{
		Key:         key,
		RequestHash: requestHash,
		Ready:       ready,
	}
	if ready {
		rec.StatusCode = *code
		rec.Body = json.RawMessage(*bodyNullable)
	}
	return rec, nil
}

func (r *IdempotencyRepository) SetResponse(ctx context.Context, tx idempotency.Tx, key idempotency.Key, requestHash string, status int, body json.RawMessage) error {
	tag, err := tx.Exec(ctx,
		`UPDATE idempotency_records
         SET response_code = $3, response_body = $4, updated_at = now()
         WHERE idempotency_key = $1 AND request_hash = $2`,
		string(key), requestHash, status, body,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("idempotency response update affected %d rows", tag.RowsAffected())
	}
	return nil
}

var _ idempotency.Store = (*IdempotencyRepository)(nil)
