package repository

import (
	"context"

	"github.com/malem-mayeng/wallet-transfer-assignment/internal/domain"
)

type Repository interface {
	WithTx(
		ctx context.Context,
		fn func(context.Context, TransferStore) error,
	) error

	GetTransferByIdempotencyKey(
		ctx context.Context,
		idempotencyKey string,
	) (*domain.Transfer, error)
}

type TransferStore interface {
	CreateOrGetTransfer(
		ctx context.Context,
		transfer *domain.Transfer,
	) (*domain.Transfer, bool, error)

	GetWalletForUpdate(
		ctx context.Context,
		walletID string,
	) (*domain.Wallet, error)

	UpdateWalletBalance(
		ctx context.Context,
		walletID string,
		balance int64,
	) error

	CreateLedgerEntry(
		ctx context.Context,
		entry *domain.LedgerEntry,
	) error

	UpdateTransferStatus(
		ctx context.Context,
		transferID int64,
		status domain.TransferStatus,
	) error
}
