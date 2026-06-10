package repository

import (
	"context"
	"time"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
)

// TransactionManager defines an abstract transaction boundary.
//
// The application/service layer owns transaction scope and uses this interface
// to execute multiple repository calls in one atomic unit. Repository methods
// accept a context and do not open or commit transactions on their own.
type TransactionManager interface {
	RunInTransaction(ctx context.Context, fn func(ctx context.Context) error) error
}

// WalletRepository provides persistence operations for wallet aggregates.
//
// Responsibilities:
// - load wallet state for business logic
// - persist balance and version changes
// - expose lock-aware accessors when necessary
// - avoid business rule enforcement; that belongs in the domain/application layer
type WalletRepository interface {
	Create(ctx context.Context, wallet *domain.Wallet) error
	GetByID(ctx context.Context, walletID string) (*domain.Wallet, error)
	GetByIDForUpdate(ctx context.Context, walletID string) (*domain.Wallet, error)
	Update(ctx context.Context, wallet *domain.Wallet) error
}

// TransferRepository provides persistence operations for transfer workflows.
//
// Responsibilities:
// - store new transfer requests and state
// - look up transfers by ID or idempotency key
// - update transfer status and failure reason
// - keep transfer persistence isolated from business orchestration
type TransferRepository interface {
	Create(ctx context.Context, transfer *domain.Transfer) error
	GetByID(ctx context.Context, transferID string) (*domain.Transfer, error)
	GetByIdempotencyKey(ctx context.Context, idempotencyKey string) (*domain.Transfer, error)
	UpdateStatus(ctx context.Context, transferID string, status domain.TransferStatus, failureReason string) error
}

// LedgerRepository provides persistence operations for ledger entries.
//
// Responsibilities:
// - record debit and credit entries for completed transfers
// - validate that ledger entries are persisted as a unit
// - support retrieval by transfer or wallet for reconciliation and testing
type LedgerRepository interface {
	CreateEntries(ctx context.Context, entries []domain.LedgerEntry) error
	GetByTransferID(ctx context.Context, transferID string) ([]domain.LedgerEntry, error)
	GetByWalletID(ctx context.Context, walletID string) ([]domain.LedgerEntry, error)
}

// IdempotencyStatus represents the current state of an idempotency record.
type IdempotencyStatus string

const (
	IdempotencyStatusPending   IdempotencyStatus = "PENDING"
	IdempotencyStatusCompleted IdempotencyStatus = "COMPLETED"
	IdempotencyStatusFailed    IdempotencyStatus = "FAILED"
)

// IdempotencyRecord represents stored idempotency metadata.
//
// This type is intentionally defined in the repository package because the
// record is a persistence concern that supports idempotency workflows.
type IdempotencyRecord struct {
	IdempotencyKey string
	TransferID     string
	RequestHash    string
	Status         IdempotencyStatus
	ResponseJSON   []byte
	StatusCode     int
	CreatedAt      time.Time
	// Actor metadata
	CreatedBy string
	CallerIP  string
	UserAgent string
}

// IdempotencyRepository provides persistence for request deduplication.
//
// Responsibilities:
// - persist the idempotency key and related transfer linkage
// - store the canonical response for repeated requests
// - detect same-key duplicates and support payload fingerprinting
type IdempotencyRepository interface {
	Create(ctx context.Context, record *IdempotencyRecord) error
	GetByKey(ctx context.Context, idempotencyKey string) (*IdempotencyRecord, error)
	UpdateResponse(ctx context.Context, idempotencyKey string, responseJSON []byte, statusCode int, status IdempotencyStatus) error
	UpdateStatus(ctx context.Context, idempotencyKey string, status IdempotencyStatus) error
	// CleanupPending transitions or removes stale PENDING records older than cutoff.
	// Returns the number of records affected.
	CleanupPending(ctx context.Context, cutoff time.Time) (int, error)
}
