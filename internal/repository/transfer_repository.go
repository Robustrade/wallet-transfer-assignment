package repository

import (
	"context"
	"database/sql"

	"wallet-transfer/internal/domain"
)

type TransferRepository struct {
	db *sql.DB
}

func NewTransferRepository(db *sql.DB) *TransferRepository {
	return &TransferRepository{db: db}
}

func (r *TransferRepository) CreateTransfer(
	ctx context.Context,
	transfer *domain.Transfer,
) error {
	// TODO: Persist the transfer.
	return domain.ErrNotImplemented
}

func (r *TransferRepository) GetTransferByIdempotencyKey(
	ctx context.Context,
	key string,
) (*domain.Transfer, error) {
	// TODO: Retrieve a transfer using its idempotency key.
	return nil, domain.ErrNotImplemented
}
