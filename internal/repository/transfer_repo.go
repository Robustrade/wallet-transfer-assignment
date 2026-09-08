package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"wallet-transfer-assignment/internal/domain"
	"wallet-transfer-assignment/internal/repository/db"
)

type TransferRepository interface {
	CreateTransfer(ctx context.Context, transfer domain.Transfer) (*domain.Transfer, error)
	GetTransfer(ctx context.Context, id string) (*domain.Transfer, error)
}

type PostgresTransferRepository struct {
	pool *pgxpool.Pool
}

func NewTransferRepository(pool *pgxpool.Pool) *PostgresTransferRepository {
	return &PostgresTransferRepository{
		pool: pool,
	}
}

func (r *PostgresTransferRepository) q(ctx context.Context) *db.Queries {
	return db.New(getDBTX(ctx, r.pool))
}

func (r *PostgresTransferRepository) CreateTransfer(ctx context.Context, t domain.Transfer) (*domain.Transfer, error) {
	var errReason pgtype.Text
	if t.ErrorReason != nil {
		errReason = pgtype.Text{String: *t.ErrorReason, Valid: true}
	}

	row, err := r.q(ctx).CreateTransfer(ctx, db.CreateTransferParams{
		ID:             t.ID,
		IdempotencyKey: t.IdempotencyKey,
		FromWalletID:   t.FromWalletID,
		ToWalletID:     t.ToWalletID,
		Amount:         t.Amount,
		Status:         string(t.Status),
		ErrorReason:    errReason,
	})
	if err != nil {
		return nil, err
	}
	return toDomainTransfer(row), nil
}

func (r *PostgresTransferRepository) GetTransfer(ctx context.Context, id string) (*domain.Transfer, error) {
	row, err := r.q(ctx).GetTransfer(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrTransferNotFound
		}
		return nil, err
	}
	return toDomainTransfer(row), nil
}

func toDomainTransfer(t db.Transfer) *domain.Transfer {
	var errReason *string
	if t.ErrorReason.Valid {
		errReason = &t.ErrorReason.String
	}
	return &domain.Transfer{
		ID:             t.ID,
		IdempotencyKey: t.IdempotencyKey,
		FromWalletID:   t.FromWalletID,
		ToWalletID:     t.ToWalletID,
		Amount:         t.Amount,
		Status:         domain.TransferStatus(t.Status),
		ErrorReason:    errReason,
		CreatedAt:      t.CreatedAt.Time,
		UpdatedAt:      t.UpdatedAt.Time,
	}
}
