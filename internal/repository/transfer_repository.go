package repository

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"wallet-transfer-service/internal/domain"
)

type TransferRepository interface {
	CreateIfNotExists(
		ctx context.Context,
		tx pgx.Tx,
		transfer *domain.Transfer,
	) (bool, error)

	GetByIdempotencyKey(
		ctx context.Context,
		tx pgx.Tx,
		key string,
	) (*domain.Transfer, error)

	GetByID(
		ctx context.Context,
		tx pgx.Tx,
		id uuid.UUID,
	) (*domain.Transfer, error)

	UpdateState(
		ctx context.Context,
		tx pgx.Tx,
		transferID uuid.UUID,
		state domain.TransferState,
	) error
}
