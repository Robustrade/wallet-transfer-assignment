package repository

import (
	"context"
	"errors"

	"github.com/Robustrade/wallet-transfer-assignment/internal/model"
)

var (
	ErrNotFound            = errors.New("record not found")
	ErrInsufficientFunds   = errors.New("insufficient funds")
	ErrIdempotencyConflict = errors.New("idempotency key already exists")
)

type Repository interface {
	// ExecuteTransferTx handles the entire transfer atomically.
	ExecuteTransferTx(ctx context.Context, transfer *model.Transfer) error

	// GetTransferByIdempotencyKey fetches an existing transfer for idempotency logic.
	GetTransferByIdempotencyKey(ctx context.Context, key string) (*model.Transfer, error)

	// Admin / setup helpers
	GetWallet(ctx context.Context, id string) (*model.Wallet, error)
	CreateWallet(ctx context.Context, wallet *model.Wallet) error
	GetTransfersByWalletID(ctx context.Context, walletID string) ([]*model.Transfer, error)
}
