package repositories

import (
	"database/sql"
	"wallet-transfer-service/internal/models"
)

// Repository interfaces define data access operations

// WalletRepository handles wallet data operations
type WalletRepository interface {
	GetByID(tx *sql.Tx, id string) (*models.Wallet, error)
	GetByIDForUpdate(tx *sql.Tx, id string) (*models.Wallet, error)
	UpdateBalance(tx *sql.Tx, id string, amount float64, isCredit bool) error
	Create(tx *sql.Tx, wallet *models.Wallet) error
}

// TransferRepository handles transfer data operations
type TransferRepository interface {
	Create(tx *sql.Tx, transfer *models.Transfer) error
	GetByID(tx *sql.Tx, id string) (*models.Transfer, error)
	UpdateStatus(tx *sql.Tx, id string, status models.TransferStatus) error
}

// LedgerRepository handles ledger entry operations
type LedgerRepository interface {
	CreateEntry(tx *sql.Tx, entry *models.LedgerEntry) error
	GetEntriesByTransferID(tx *sql.Tx, transferID string) ([]*models.LedgerEntry, error)
}

// IdempotencyRepository handles idempotency record operations
type IdempotencyRepository interface {
	Create(tx *sql.Tx, record *models.IdempotencyRecord) error
	GetByKey(tx *sql.Tx, key string) (*models.IdempotencyRecord, error)
	Update(tx *sql.Tx, record *models.IdempotencyRecord) error
}

// TransactionManager handles database transactions
type TransactionManager interface {
	BeginTx() (*sql.Tx, error)
	Commit(tx *sql.Tx) error
	Rollback(tx *sql.Tx) error
}
