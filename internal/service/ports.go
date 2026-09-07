package service

import (
	"context"

	"github.com/gaurav2204bits-collab/wallet-transfer-assignment/internal/domain"
)

// The service owns these interfaces (dependency inversion): concrete
// implementations live in the repository and platform packages and are wired in
// main. Every method takes a context.Context so a repository can pull an
// in-flight transaction handle from it (see TxManager).

// IdempotencyRecord is the stored association between an idempotency key and the
// transfer it produced, plus a hash of the originating request.
type IdempotencyRecord struct {
	Key         string
	TransferID  string
	RequestHash string
}

// IdempotencyRepository persists idempotency keys durably.
type IdempotencyRepository interface {
	// Insert claims the key. It must return domain.ErrDuplicateIdempotencyKey
	// (and write nothing) if the key already exists — this is the exactly-once
	// guarantee and must be backed by a unique constraint.
	Insert(ctx context.Context, rec IdempotencyRecord) error
	// GetRecord returns the record for a key, or domain.ErrTransferNotFound if
	// absent.
	GetRecord(ctx context.Context, key string) (IdempotencyRecord, error)
}

// WalletRepository persists wallets.
type WalletRepository interface {
	// GetForUpdate reads a wallet and takes a row-level lock for the remainder
	// of the transaction. Only meaningful inside TxManager.WithinTx.
	GetForUpdate(ctx context.Context, id string) (*domain.Wallet, error)
	// Get reads a wallet without locking.
	Get(ctx context.Context, id string) (*domain.Wallet, error)
	// UpdateBalance persists a new balance and bumps the wallet version.
	UpdateBalance(ctx context.Context, w *domain.Wallet) error
}

// TransferRepository persists transfers.
type TransferRepository interface {
	Create(ctx context.Context, t *domain.Transfer) error
	GetByID(ctx context.Context, id string) (*domain.Transfer, error)
	GetByIdempotencyKey(ctx context.Context, key string) (*domain.Transfer, error)
	UpdateState(ctx context.Context, t *domain.Transfer) error
}

// LedgerRepository persists ledger entries.
type LedgerRepository interface {
	// InsertPair writes the debit and credit entries for a transfer atomically.
	// The (transfer_id, type) uniqueness constraint makes a retry a no-op error
	// the caller can treat as already-done.
	InsertPair(ctx context.Context, debit, credit domain.LedgerEntry) error
	// SumByWallet returns net position (credits - debits) for a wallet; used by
	// tests and reconciliation, not the hot path.
	SumByWallet(ctx context.Context, walletID string) (domain.Money, error)
}

// TxManager runs fn inside a single database transaction. Implementations put a
// transaction handle on the context passed to fn; repositories called within fn
// detect it and use that handle, so all writes commit or roll back together.
type TxManager interface {
	WithinTx(ctx context.Context, fn func(ctx context.Context) error) error
}

// IDGenerator produces unique identifiers for new transfers.
type IDGenerator interface {
	NewID() string
}
