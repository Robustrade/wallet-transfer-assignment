package service

import (
	"context"

	"github.com/shopspring/decimal"

	"github.com/Nandukrishna-S/wallet-transfer-assignment/internal/domain"
	"github.com/Nandukrishna-S/wallet-transfer-assignment/internal/repository"
)

// WalletRepository is the persistence contract TransferService needs for
// wallets. Implemented by internal/repository/postgres.WalletRepo.
type WalletRepository interface {
	GetForUpdate(ctx context.Context, tx repository.Tx, walletIDs ...string) ([]domain.Wallet, error)
	UpdateBalances(ctx context.Context, tx repository.Tx, fromWalletID string, fromBalance decimal.Decimal, toWalletID string, toBalance decimal.Decimal) error
}

// TransferRepository is the persistence contract TransferService needs
// for transfers and their ledger entries. Implemented by
// internal/repository/postgres.TransferRepo.
type TransferRepository interface {
	InsertTransfer(ctx context.Context, tx repository.Tx, id int64, requestHash, fromWalletID, toWalletID string, amount decimal.Decimal, status domain.Status, failureReason *domain.ErrorCode) (*domain.Transfer, error)
	FindByID(ctx context.Context, id int64) (*domain.Transfer, error)
	InsertLedgerEntries(ctx context.Context, tx repository.Tx, transferID int64, fromWalletID, toWalletID string, amount decimal.Decimal) error
}

// Cache is the read-through cache contract for transfers: a single
// transfer:{id} key, written once, never updated — immutability makes
// self-healing on TTL expiry safe with no update path to maintain.
// Implemented by internal/repository/cache.TransferCache.
type Cache interface {
	Get(ctx context.Context, id int64) (*domain.Transfer, error)
	Set(ctx context.Context, id int64, transfer *domain.Transfer) error
}

// IDGenerator mints new transfer IDs. Implemented by *idgen.Generator.
type IDGenerator interface {
	NextID() (int64, error)
}

// TransferRequest is the already-shape-validated input to
// TransferService.Transfer — the handler has already parsed and
// type-checked it (malformed JSON, bad UUIDs, bad amount format all
// rejected with 400 before this is built), so the service only ever
// deals with business-meaning validation from here on.
type TransferRequest struct {
	ID               int64
	FromWalletID     string
	ToWalletID       string
	Amount           decimal.Decimal
	AmountMinorUnits int64
}
