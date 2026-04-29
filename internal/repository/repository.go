package repository

import (
	"context"
	"fmt"
	"time"
	"wallet-transfer/internal/domain"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Repository interface defines all database operations
type Repository interface {
	// Transaction management
	BeginTx(ctx context.Context) (*gorm.DB, error)
	CommitTx(ctx context.Context, tx *gorm.DB) error
	RollbackTx(ctx context.Context, tx *gorm.DB) error

	// Wallet operations
	GetWalletsForUpdate(ctx context.Context, tx *gorm.DB, walletIDs []uuid.UUID) ([]*domain.Wallet, error)
	UpdateWalletBalance(ctx context.Context, tx *gorm.DB, walletID uuid.UUID, newBalance int64) error

	// Transfer operations
	CreateTransfer(ctx context.Context, tx *gorm.DB, transfer *domain.Transfer) error
	UpdateTransferState(ctx context.Context, tx *gorm.DB, transferID uuid.UUID, state domain.TransferState) error
	GetTransferByID(ctx context.Context, transferID uuid.UUID) (*domain.Transfer, error)

	// Ledger operations
	CreateLedgerEntry(ctx context.Context, tx *gorm.DB, entry *domain.LedgerEntry) error

	// Idempotency operations
	AcquireIdempotencyLock(ctx context.Context, tx *gorm.DB, key uuid.UUID) error
	GetIdempotencyRecord(ctx context.Context, key uuid.UUID) (*domain.IdempotencyRecord, error)
	CreateIdempotencyRecord(ctx context.Context, tx *gorm.DB, record *domain.IdempotencyRecord) error
}

type PostgresRepository struct {
	db *gorm.DB
}

func NewPostgresRepository(db *gorm.DB) *PostgresRepository {
	return &PostgresRepository{db: db}
}

// BeginTx starts a new transaction
func (r *PostgresRepository) BeginTx(ctx context.Context) (*gorm.DB, error) {
	tx := r.db.WithContext(ctx).Begin()
	if tx.Error != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", tx.Error)
	}
	return tx, nil
}

// CommitTx commits the provided transaction
func (r *PostgresRepository) CommitTx(ctx context.Context, tx *gorm.DB) error {
	if tx == nil {
		return fmt.Errorf("nil transaction")
	}
	if c := tx.Commit(); c != nil && c.Error != nil {
		return fmt.Errorf("failed to commit transaction: %w", c.Error)
	}
	return nil
}

// RollbackTx rollbacks the provided transaction
func (r *PostgresRepository) RollbackTx(ctx context.Context, tx *gorm.DB) error {
	if tx == nil {
		return fmt.Errorf("nil transaction")
	}
	if rdb := tx.Rollback(); rdb != nil && rdb.Error != nil {
		return fmt.Errorf("failed to rollback transaction: %w", rdb.Error)
	}
	return nil
}

// GetWalletsForUpdate locks and retrieves wallets for update
func (r *PostgresRepository) GetWalletsForUpdate(ctx context.Context, tx *gorm.DB, walletIDs []uuid.UUID) ([]*domain.Wallet, error) {
	if len(walletIDs) == 0 {
		return nil, fmt.Errorf("no wallet IDs provided")
	}

	var wallets []*domain.Wallet

	// Use GORM's Clauses for SELECT ... FOR UPDATE with ordering
	err := tx.WithContext(ctx).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id IN ?", walletIDs).
		Order("id").
		Find(&wallets).Error

	if err != nil {
		return nil, fmt.Errorf("failed to lock wallets: %w", err)
	}

	if len(wallets) != len(walletIDs) {
		return nil, domain.ErrWalletNotFound
	}

	return wallets, nil
}

// UpdateWalletBalance updates a wallet's balance
func (r *PostgresRepository) UpdateWalletBalance(ctx context.Context, tx *gorm.DB, walletID uuid.UUID, newBalance int64) error {
	result := tx.WithContext(ctx).
		Model(&domain.Wallet{}).
		Where("id = ?", walletID).
		Updates(map[string]interface{}{
			"balance":    newBalance,
			"updated_at": time.Now(),
		})

	if result.Error != nil {
		return fmt.Errorf("failed to update wallet balance: %w", result.Error)
	}

	if result.RowsAffected == 0 {
		return domain.ErrWalletNotFound
	}

	return nil
}

// CreateTransfer creates a new transfer record
func (r *PostgresRepository) CreateTransfer(ctx context.Context, tx *gorm.DB, transfer *domain.Transfer) error {
	if err := tx.WithContext(ctx).Create(transfer).Error; err != nil {
		return fmt.Errorf("failed to create transfer: %w", err)
	}
	return nil
}

// UpdateTransferState updates the state of a transfer
func (r *PostgresRepository) UpdateTransferState(ctx context.Context, tx *gorm.DB, transferID uuid.UUID, state domain.TransferState) error {
	result := tx.WithContext(ctx).
		Model(&domain.Transfer{}).
		Where("id = ?", transferID).
		Updates(map[string]interface{}{
			"state":      state,
			"updated_at": time.Now(),
		})

	if result.Error != nil {
		return fmt.Errorf("failed to update transfer state: %w", result.Error)
	}

	if result.RowsAffected == 0 {
		return domain.ErrTransferNotFound
	}

	return nil
}

// GetTransferByID retrieves a transfer by ID
func (r *PostgresRepository) GetTransferByID(ctx context.Context, transferID uuid.UUID) (*domain.Transfer, error) {
	var transfer domain.Transfer

	err := r.db.WithContext(ctx).
		Where("id = ?", transferID).
		First(&transfer).Error

	if err == gorm.ErrRecordNotFound {
		return nil, domain.ErrTransferNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get transfer: %w", err)
	}

	return &transfer, nil
}

// CreateLedgerEntry creates a new ledger entry
func (r *PostgresRepository) CreateLedgerEntry(ctx context.Context, tx *gorm.DB, entry *domain.LedgerEntry) error {
	if err := tx.WithContext(ctx).Create(entry).Error; err != nil {
		return fmt.Errorf("failed to create ledger entry: %w", err)
	}
	return nil
}

// AcquireIdempotencyLock acquires an exclusive advisory lock on the idempotency key
// This blocks concurrent requests with the same key until the transaction commits/rolls back
func (r *PostgresRepository) AcquireIdempotencyLock(ctx context.Context, tx *gorm.DB, key uuid.UUID) error {
	// Use PostgreSQL's hashtext function to convert UUID string to int64 for advisory lock
	if err := tx.WithContext(ctx).Exec("SELECT pg_advisory_xact_lock(hashtext(?))", key.String()).Error; err != nil {
		return fmt.Errorf("failed to acquire advisory lock: %w", err)
	}

	return nil
}

// GetIdempotencyRecord retrieves an idempotency record by key
func (r *PostgresRepository) GetIdempotencyRecord(ctx context.Context, key uuid.UUID) (*domain.IdempotencyRecord, error) {
	var record domain.IdempotencyRecord

	err := r.db.WithContext(ctx).
		Where("idempotency_key = ?", key).
		First(&record).Error

	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get idempotency record: %w", err)
	}

	return &record, nil
}

// CreateIdempotencyRecord creates a new idempotency record
func (r *PostgresRepository) CreateIdempotencyRecord(ctx context.Context, tx *gorm.DB, record *domain.IdempotencyRecord) error {
	if err := tx.WithContext(ctx).Create(record).Error; err != nil {
		return fmt.Errorf("failed to create idempotency record: %w", err)
	}
	return nil
}
