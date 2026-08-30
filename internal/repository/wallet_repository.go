package repository

import (
	"context"

	"wallet-transfer/internal/domain"

	"github.com/google/uuid"
)

type WalletRepository interface {
	GetByID(ctx context.Context, walletID uuid.UUID) (*domain.Wallet, error)
}
