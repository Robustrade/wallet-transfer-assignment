package repository

import (
	"context"

	"github.com/google/uuid"
	"wallet-transfer-assignment/internal/domain"
)

type WalletRepository interface {
	FindByID(
		ctx context.Context,
		id uuid.UUID,
	) (*domain.Wallet, error)

	FindByIDsForUpdate(
		ctx context.Context,
		ids ...uuid.UUID,
	) ([]*domain.Wallet, error)

	Debit(
		ctx context.Context,
		id uuid.UUID,
		amount int64,
	) error

	Credit(
		ctx context.Context,
		id uuid.UUID,
		amount int64,
	) error
}
