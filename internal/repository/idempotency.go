package repository

import (
	"context"
	"fmt"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
)

func (c *queries) CreateIdempotencyRecord(ctx context.Context, r *domain.IdempotencyRecord) error {
	_, err := c.exec(ctx,
		`INSERT INTO idempotency_records (key, request_hash, transfer_id, created_at) VALUES (?, ?, ?, ?)`,
		r.Key, r.RequestHash, r.TransferID, r.CreatedAt)
	if err != nil {
		if c.db.IsUniqueViolation(err) {
			return fmt.Errorf("%w: %s", domain.ErrDuplicateKey, r.Key)
		}
		return fmt.Errorf("insert idempotency record %s: %w", r.Key, err)
	}
	return nil
}

func (c *queries) GetIdempotencyRecord(ctx context.Context, key string) (*domain.IdempotencyRecord, error) {
	var r domain.IdempotencyRecord
	err := c.queryRow(ctx,
		`SELECT key, request_hash, transfer_id, created_at FROM idempotency_records WHERE key = ?`, key).
		Scan(&r.Key, &r.RequestHash, &r.TransferID, &r.CreatedAt)
	if err != nil {
		if noRows(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("select idempotency record %s: %w", key, err)
	}
	return &r, nil
}
