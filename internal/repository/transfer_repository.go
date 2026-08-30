package repository

import (
	"context"

	"github.com/jackc/pgx/v5"

	"wallet-transfer-assignment/internal/domain"
)

type TransferRepository interface {
	GetByIdempotencyKey(
		ctx context.Context,
		tx pgx.Tx,
		key string,
	) (*domain.Transfer, error)

	Create(
		ctx context.Context,
		tx pgx.Tx,
		transfer *domain.Transfer,
	) error

	UpdateStatus(
		ctx context.Context,
		tx pgx.Tx,
		id string,
		status domain.TransferStatus,
	) error
}
