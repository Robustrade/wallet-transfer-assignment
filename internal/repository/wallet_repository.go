package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"wallet-transfer-assignment/internal/domain"
)

var ErrWalletNotFound = errors.New("wallet not found")

type WalletRepository interface {
	GetByID(ctx context.Context, tx pgx.Tx, id string) (*domain.Wallet, error)
	GetByIDForUpdate(ctx context.Context, tx pgx.Tx, id string) (*domain.Wallet, error)
	UpdateBalance(ctx context.Context, tx pgx.Tx, id string, balance int64) error
}
