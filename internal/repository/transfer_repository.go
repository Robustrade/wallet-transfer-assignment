package repository

import (
	"context"

	"wallet-transfer-assignment/internal/domain"

	"github.com/google/uuid"
)

type TransferRepository interface {
	FindByIdempotencyKey(
		ctx context.Context,
		key string,
	) (*domain.Transfer, error)

	Create(
		ctx context.Context,
		transfer *domain.Transfer,
	) error

	UpdateStatus(
		ctx context.Context,
		id uuid.UUID,
		status domain.TransferStatus,
		failureReason domain.TransferFailureReason,
	) error
}
