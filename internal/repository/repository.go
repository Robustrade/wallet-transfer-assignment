// Package repository defines persistence ports used by the transfer service.
package repository

import (
	"context"

	"wallet-transfer-assignment/internal/domain"
)

// IdempotencyRecord is the stored result of one idempotency key.
// ResponseStatus is zero until the owning transaction saves a response.
type IdempotencyRecord struct {
	Key            string
	RequestHash    string
	TransferID     string
	ResponseStatus int
	ResponseBody   []byte
}

// Tx is one database transaction. The service decides what to write.
type Tx interface {
	ClaimIdempotency(ctx context.Context, key, requestHash string) (IdempotencyRecord, bool, error)
	LockIdempotency(ctx context.Context, key string) (IdempotencyRecord, bool, error)
	SaveIdempotencyResult(ctx context.Context, key string, status int, body []byte, transferID string) error
	LockWallets(ctx context.Context, ids ...string) (map[string]domain.Wallet, error)
	InsertTransfer(ctx context.Context, transfer domain.Transfer) (string, error)
	UpdateTransferStatus(ctx context.Context, id string, from, to domain.Status, reason string) error
	InsertLedgerPair(ctx context.Context, transferID, fromWalletID, toWalletID string, amount int64) error
	DebitWallet(ctx context.Context, walletID string, amount int64) error
	CreditWallet(ctx context.Context, walletID string, amount int64) error
}

// Store is the persistence API outside a transfer transaction.
type Store interface {
	CreateWallet(ctx context.Context, wallet domain.Wallet) error
	GetWallet(ctx context.Context, id string) (domain.Wallet, error)
	GetTransfer(ctx context.Context, id string) (domain.Transfer, []domain.LedgerEntry, error)
	WithTx(ctx context.Context, fn func(tx Tx) error) error
}
