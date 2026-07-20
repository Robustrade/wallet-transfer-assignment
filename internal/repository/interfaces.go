package repository

import (
	"context"

	"wallet-transfer-assignment/internal/domain"
)

type Store interface {
	WithTx(ctx context.Context, fn func(ctx context.Context, tx Tx) error) error
	GetTransfer(ctx context.Context, id string) (domain.Transfer, error)
	GetWallet(ctx context.Context, id string) (domain.Wallet, error)
	ListWalletLedger(ctx context.Context, walletID string, limit int, cursor int64) ([]domain.LedgerEntry, error)
}

type Tx interface {
	InsertIdempotencyRecord(ctx context.Context, key string, fingerprint string) (bool, error)
	GetIdempotencyRecord(ctx context.Context, key string) (domain.IdempotencyRecord, error)
	SetIdempotencyTransfer(ctx context.Context, key string, transferID string) error
	CompleteIdempotencyRecord(ctx context.Context, key string, transferID string, responseSnapshot []byte) error

	GetWallet(ctx context.Context, id string) (domain.Wallet, error)
	InsertPendingTransfer(ctx context.Context, transfer domain.Transfer) (domain.Transfer, error)
	LockTransferForProcessing(ctx context.Context, id string) (domain.Transfer, error)
	CompleteTransfer(ctx context.Context, transfer domain.Transfer) error

	LockWalletsForTransfer(ctx context.Context, fromWalletID string, toWalletID string) (domain.Wallet, domain.Wallet, error)
	UpdateWalletBalances(ctx context.Context, fromWallet domain.Wallet, toWallet domain.Wallet) error

	InsertLedgerEntries(ctx context.Context, entries []domain.LedgerEntry) error
}
