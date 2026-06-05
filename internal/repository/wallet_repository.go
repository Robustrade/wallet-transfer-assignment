package repository

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"wallet-transfer-service/internal/domain"
)

type WalletRepository interface {
	GetByID(
		ctx context.Context,
		tx pgx.Tx,
		id uuid.UUID,
	) (*domain.Wallet, error)

	GetForUpdate(
		ctx context.Context,
		tx pgx.Tx,
		id uuid.UUID,
	) (*domain.Wallet, error)

	UpdateBalance(
		ctx context.Context,
		tx pgx.Tx,
		id uuid.UUID,
		balance int64,
	) error
}
