package repository

import (
	"context"

	"github.com/google/uuid"
	"github.com/trjade/wallet-transfer-service/internal/domain"
)

type TxManager interface {
	WithTx(ctx context.Context, fn func(txCtx context.Context) error) error
}

type WalletRepository interface {
	GetWalletByID(ctx context.Context, walletID uuid.UUID) (*domain.Wallet, error)
	GetWalletsForUpdate(ctx context.Context, walletIDs []uuid.UUID) ([]domain.Wallet, error)
	UpdateWallet(ctx context.Context, walletID uuid.UUID, newBalance int64) error
}

type TransferRepository interface {
	CreateTransfer(ctx context.Context, transfer *domain.Transfer) error
	GetTransferByIdempotencyKey(ctx context.Context, idempotencyKey string) (*domain.Transfer, error)
	UpdateTransferStatus(ctx context.Context, transferID uuid.UUID, newStatus domain.TransferStatus) error
}

type LedgerRepository interface {
	Create(ctx context.Context, entry *domain.LedgerEntry) error
}
