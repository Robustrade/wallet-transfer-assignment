package repository

import (
	"context"
	"errors"
)

var ErrIdempotencyHashMismatch = errors.New("idempotency hash mismatch")

type Repository interface {
	ClaimIdempotency(ctx context.Context, key, hash string) (bool, error)
	GetIdempotency(ctx context.Context, key string) (string, int, error)
	UpdateIdempotency(ctx context.Context, key, status, response string, code int) error
	BeginTx(ctx context.Context) (Tx, error)
}

type Tx interface {
	Commit(ctx context.Context) error
	Rollback(ctx context.Context) error

	GetBalance(ctx context.Context, walletID int64) (int64, error) // 👈 THIS

	GetWalletForUpdate(ctx context.Context, id int64) (int64, error)
	UpdateWallet(ctx context.Context, id int64, balance int64) error

	CreateTransfer(ctx context.Context, from, to, amount int64, key string) (int64, error)
	UpdateTransferState(ctx context.Context, transferID int64, state string) error

	InsertLedgerEntry(ctx context.Context, walletID, transferID int64, typ string, amount int64) error
}
