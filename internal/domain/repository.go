package domain

import "context"

type IWalletRepository interface {
	GetByID(ctx context.Context, id string) (*Wallet, error)
}

type ITransferRepository interface {
	GetByIdempotencyKey(ctx context.Context, key string) (*Transfer, error)
	ExecuteTransfer(ctx context.Context, idempotencyKey, fromWalletID, toWalletID string, amount float64, timestamp int64) (*Transfer, error)
	ListPendingTransfers(ctx context.Context) ([]*Transfer, error)
	ResolvePendingTransfer(ctx context.Context, transfer *Transfer) (*Transfer, error)
	ListByWallet(ctx context.Context, walletID string, limit, offset int32) ([]*Transfer, error)
}
