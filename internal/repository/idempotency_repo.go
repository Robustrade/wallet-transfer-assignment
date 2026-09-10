package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"wallet-transfer-assignment/internal/domain"
	"wallet-transfer-assignment/internal/repository/db"
)

type IdempotencyRepository interface {
	GetIdempotencyRecord(ctx context.Context, key string) (*domain.IdempotencyRecord, error)
	CreateIdempotencyRecord(ctx context.Context, record domain.IdempotencyRecord) (*domain.IdempotencyRecord, error)
}

type PostgresIdempotencyRepository struct {
	pool *pgxpool.Pool
}

func NewIdempotencyRepository(pool *pgxpool.Pool) *PostgresIdempotencyRepository {
	return &PostgresIdempotencyRepository{
		pool: pool,
	}
}

func (r *PostgresIdempotencyRepository) q(ctx context.Context) *db.Queries {
	return db.New(getDBTX(ctx, r.pool))
}

func (r *PostgresIdempotencyRepository) GetIdempotencyRecord(ctx context.Context, key string) (*domain.IdempotencyRecord, error) {
	row, err := r.q(ctx).GetIdempotencyRecord(ctx, key)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return toDomainIdempotencyRecord(row), nil
}

func (r *PostgresIdempotencyRepository) CreateIdempotencyRecord(ctx context.Context, rec domain.IdempotencyRecord) (*domain.IdempotencyRecord, error) {
	row, err := r.q(ctx).CreateIdempotencyRecord(ctx, db.CreateIdempotencyRecordParams{
		Key:          rec.Key,
		RequestHash:  rec.RequestHash,
		ResponseCode: int32(rec.ResponseCode),
		ResponseBody: rec.ResponseBody,
	})
	if err != nil {
		return nil, err
	}
	return toDomainIdempotencyRecord(row), nil
}

func toDomainIdempotencyRecord(r db.IdempotencyRecord) *domain.IdempotencyRecord {
	return &domain.IdempotencyRecord{
		Key:          r.Key,
		RequestHash:  r.RequestHash,
		ResponseCode: int(r.ResponseCode),
		ResponseBody: r.ResponseBody,
		CreatedAt:    r.CreatedAt.Time,
	}
}
