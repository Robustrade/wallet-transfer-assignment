package repository

import (
	"context"
	"database/sql"

	"wallet-transfer-assignment/internal/domain"
)

type WalletRepository interface {
	GetWalletForUpdate(ctx context.Context, tx *sql.Tx, walletId string) (*domain.Wallet, error)
	UpdateBalance(ctx context.Context, tx *sql.Tx, walletId string, balance int64) error
}

type TransferRepository interface {
	Create(ctx context.Context, tx *sql.Tx, transfer *domain.Transfer) error
	GetById(ctx context.Context, tx *sql.Tx, transferId string) (*domain.Transfer, error)
	UpdateStatus(ctx context.Context, tx *sql.Tx, trasferId string, status domain.TransferStatus) error
}

type LedgerRepository interface {
	Create(ctx context.Context, tx *sql.Tx, entry *domain.LedgerEntry) error
}

type IdempotencyRepository interface {
	Create(ctx context.Context, tx *sql.Tx, id, key string, transferId string) error
	GetByKey(ctx context.Context, tx *sql.Tx, key string) (string, error)
}
