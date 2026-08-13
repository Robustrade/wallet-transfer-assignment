// Package repository defines the persistence ports the service layer
// depends on. Concrete implementations live in sub-packages (e.g. postgres);
// the service layer never imports a driver directly, which keeps it testable
// with in-memory fakes.
package repository

import (
	"context"

	"github.com/robustrade/wallet-transfer-assignment/internal/domain"
)

// TxManager runs fn inside a single database transaction. Repository calls
// made with the ctx passed into fn participate in that transaction; if fn
// returns an error the transaction is rolled back, otherwise it is committed.
type TxManager interface {
	WithinTx(ctx context.Context, fn func(ctx context.Context) error) error
}

type WalletRepository interface {
	Create(ctx context.Context, wallet *domain.Wallet) error
	Get(ctx context.Context, id string) (*domain.Wallet, error)
	// GetForUpdate locks the wallet row (SELECT ... FOR UPDATE) and must only
	// be called inside a transaction started by TxManager.
	GetForUpdate(ctx context.Context, id string) (*domain.Wallet, error)
	UpdateBalance(ctx context.Context, id string, newBalance int64) error
}

type TransferRepository interface {
	Create(ctx context.Context, transfer *domain.Transfer) error
	UpdateStatus(ctx context.Context, id string, status domain.TransferStatus, failureReason string) error
	GetByID(ctx context.Context, id string) (*domain.Transfer, error)
}

type LedgerRepository interface {
	InsertEntries(ctx context.Context, entries []domain.LedgerEntry) error
	ListByWallet(ctx context.Context, walletID string) ([]domain.LedgerEntry, error)
}

type IdempotencyRepository interface {
	// Claim attempts to atomically create a PENDING record for key. It
	// returns the record that now exists for key (whether it created it or
	// not) and whether the caller is the one who created it.
	Claim(ctx context.Context, key, requestHash string) (record *domain.IdempotencyRecord, claimed bool, err error)
	Get(ctx context.Context, key string) (*domain.IdempotencyRecord, error)
	// Complete marks a claimed key as done and links it to the transfer that
	// was durably written, so future duplicates can replay by re-fetching
	// that transfer.
	Complete(ctx context.Context, key, transferID string) error
	// Release gives up a PENDING claim that did not result in a durable
	// transfer (e.g. the target wallet didn't exist), freeing the key for a
	// future retry instead of wedging it.
	Release(ctx context.Context, key string) error
}
