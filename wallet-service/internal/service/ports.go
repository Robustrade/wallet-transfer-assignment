package service

import (
	"context"

	"github.com/shubh/wallet-service/internal/domain"
)

// WalletRepository defines operations for wallet entities.
type WalletRepository interface {
	GetByID(ctx context.Context, id string) (*domain.Wallet, error)
	LockWalletsForTransfer(ctx context.Context, walletID1, walletID2 string) (*domain.Wallet, *domain.Wallet, error)
	UpdateBalance(ctx context.Context, walletID string, newBalance int64) error
}

// TransferRepository defines operations for transfer entities.
type TransferRepository interface {
	FindByIdempotencyKey(ctx context.Context, key string) (*domain.Transfer, error)
	Create(ctx context.Context, t *domain.Transfer) error
	GetByID(ctx context.Context, id string) (*domain.Transfer, error)
}

// LedgerRepository defines operations for ledger entities.
type LedgerRepository interface {
	CreatePair(ctx context.Context, transferID string, fromWalletID, toWalletID string, amount int64) error
	GetByTransferID(ctx context.Context, transferID string) ([]domain.LedgerEntry, error)
}

// TxManager defines a transaction boundary manager.
type TxManager interface {
	RunInTx(ctx context.Context, fn func(ctx context.Context) error) error
}
