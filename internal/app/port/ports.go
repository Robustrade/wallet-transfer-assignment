package port

import (
	"context"
	"time"

	"github.com/amantiwari/wallet-transfer/internal/domain"
)

// IdempotencyRecord is the durable claim for one client key.
// TransferID is empty until the transfer row exists inside the same transaction.
type IdempotencyRecord struct {
	Key         string
	RequestHash string
	TransferID  string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// WalletRepository is the transactional wallet persistence port.
type WalletRepository interface {
	Create(ctx context.Context, wallet domain.Wallet) error
	Lock(ctx context.Context, id string) (domain.Wallet, error)
	Debit(ctx context.Context, id string, amount int64, at time.Time) error
	Credit(ctx context.Context, id string, amount int64, at time.Time) error
}

// TransferRepository is the transactional transfer persistence port.
type TransferRepository interface {
	Insert(ctx context.Context, transfer domain.Transfer) error
	GetByID(ctx context.Context, id string) (domain.Transfer, error)
	// Transition updates the row only when its current status equals from.
	Transition(ctx context.Context, updated domain.Transfer, from domain.Status) error
}

// LedgerRepository writes and reads ledger rows inside a transaction.
type LedgerRepository interface {
	InsertPair(ctx context.Context, debit, credit domain.LedgerEntry) error
}

// IdempotencyRepository claims and completes idempotency keys.
type IdempotencyRepository interface {
	// TryInsert inserts the record. inserted is true when this transaction created it.
	// When inserted is false, the returned record is the previously committed one.
	// A concurrent in-flight insert of the same key blocks until that transaction ends.
	TryInsert(ctx context.Context, record IdempotencyRecord) (IdempotencyRecord, bool, error)
	AttachTransfer(ctx context.Context, key, transferID string, at time.Time) error
}

// Repositories is the set of ports bound to one database transaction.
type Repositories interface {
	Wallets() WalletRepository
	Transfers() TransferRepository
	Ledger() LedgerRepository
	Idempotency() IdempotencyRepository
}

// UnitOfWork runs fn inside a single transaction.
// fn returning an error rolls the transaction back.
type UnitOfWork interface {
	WithinTransaction(ctx context.Context, fn func(ctx context.Context, repos Repositories) error) error
}

// WalletReader loads a wallet outside a transfer transaction.
type WalletReader interface {
	GetByID(ctx context.Context, id string) (domain.Wallet, error)
}

// TransferReader loads a transfer outside a transfer transaction.
type TransferReader interface {
	GetByID(ctx context.Context, id string) (domain.Transfer, error)
}

// LedgerReader lists ledger rows for a transfer.
type LedgerReader interface {
	ListByTransferID(ctx context.Context, transferID string) ([]domain.LedgerEntry, error)
}

// Clock supplies timestamps so tests can freeze time.
type Clock interface {
	Now() time.Time
}

// IDGenerator supplies new identifiers.
type IDGenerator interface {
	NewID() string
}
