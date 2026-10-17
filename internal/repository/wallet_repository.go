package repository

import (
	"context"
	"errors"
	"strings"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// WalletRepository defines persistence operations for wallets, transfers, ledger entries, and idempotency records.
type WalletRepository interface {
	// Wallet operations
	GetWallet(ctx context.Context, id string) (*domain.Wallet, error)
	GetWalletsForUpdate(ctx context.Context, ids ...string) ([]*domain.Wallet, error)
	DebitWalletConditional(ctx context.Context, walletID string, amount int64) (int64, error)
	CreditWallet(ctx context.Context, walletID string, amount int64) error

	// Transfer operations
	CreateTransfer(ctx context.Context, transfer *domain.Transfer) error
	UpdateTransfer(ctx context.Context, transfer *domain.Transfer) error
	GetTransfer(ctx context.Context, id string) (*domain.Transfer, error)

	// Ledger operations
	CreateLedgerEntries(ctx context.Context, entries ...*domain.LedgerEntry) error
	GetLedgerEntriesByTransferID(ctx context.Context, transferID string) ([]*domain.LedgerEntry, error)

	// Idempotency operations
	GetIdempotencyRecord(ctx context.Context, key string) (*domain.IdempotencyRecord, error)
	CreateIdempotencyRecord(ctx context.Context, record *domain.IdempotencyRecord) error
	UpdateIdempotencyRecord(ctx context.Context, record *domain.IdempotencyRecord) error

	// Transaction management
	WithTransaction(ctx context.Context, fn func(txRepo WalletRepository) error) error

	// Health check
	Ping(ctx context.Context) error
}

type gormWalletRepository struct {
	db *gorm.DB
}

// NewGormWalletRepository creates a new GORM-backed implementation of WalletRepository.
func NewGormWalletRepository(db *gorm.DB) WalletRepository {
	return &gormWalletRepository{db: db}
}

func (r *gormWalletRepository) GetWallet(ctx context.Context, id string) (*domain.Wallet, error) {
	var wallet domain.Wallet
	err := r.db.WithContext(ctx).First(&wallet, "id = ?", id).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrWalletNotFound
		}
		return nil, err
	}
	return &wallet, nil
}

// GetWalletsForUpdate locks the specified wallets in deterministic primary-key order (ORDER BY id ASC)
// to eliminate deadlocks when concurrent transfers involve the same wallets in opposing directions.
func (r *gormWalletRepository) GetWalletsForUpdate(ctx context.Context, ids ...string) ([]*domain.Wallet, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var wallets []*domain.Wallet
	err := r.db.WithContext(ctx).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id IN ?", ids).
		Order("id ASC").
		Find(&wallets).Error
	if err != nil {
		return nil, err
	}
	return wallets, nil
}

// DebitWalletConditional performs a conditional atomic deduction:
// UPDATE wallets SET balance = balance - $1, updated_at = NOW() WHERE id = $2 AND balance >= $1
//
// Why parameterized SQL is necessary here:
// GORM's built-in updates can do expressions, but an explicit parameterized SQL query provides
// absolute certainty that the conditional check `balance >= amount` is evaluated atomically
// by PostgreSQL at row-update time.
// RowsAffected == 1 indicates successful debit; RowsAffected == 0 indicates insufficient funds.
func (r *gormWalletRepository) DebitWalletConditional(ctx context.Context, walletID string, amount int64) (int64, error) {
	res := r.db.WithContext(ctx).Exec(
		"UPDATE wallets SET balance = balance - ?, updated_at = NOW() WHERE id = ? AND balance >= ?",
		amount, walletID, amount,
	)
	if res.Error != nil {
		return 0, res.Error
	}
	return res.RowsAffected, nil
}

func (r *gormWalletRepository) CreditWallet(ctx context.Context, walletID string, amount int64) error {
	res := r.db.WithContext(ctx).Exec(
		"UPDATE wallets SET balance = balance + ?, updated_at = NOW() WHERE id = ?",
		amount, walletID,
	)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return domain.ErrWalletNotFound
	}
	return nil
}

func (r *gormWalletRepository) CreateTransfer(ctx context.Context, transfer *domain.Transfer) error {
	return r.db.WithContext(ctx).Create(transfer).Error
}

func (r *gormWalletRepository) UpdateTransfer(ctx context.Context, transfer *domain.Transfer) error {
	return r.db.WithContext(ctx).Save(transfer).Error
}

func (r *gormWalletRepository) GetTransfer(ctx context.Context, id string) (*domain.Transfer, error) {
	var transfer domain.Transfer
	err := r.db.WithContext(ctx).First(&transfer, "id = ?", id).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrTransferNotFound
		}
		return nil, err
	}
	return &transfer, nil
}

func (r *gormWalletRepository) CreateLedgerEntries(ctx context.Context, entries ...*domain.LedgerEntry) error {
	if len(entries) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Create(&entries).Error
}

func (r *gormWalletRepository) GetLedgerEntriesByTransferID(ctx context.Context, transferID string) ([]*domain.LedgerEntry, error) {
	var entries []*domain.LedgerEntry
	err := r.db.WithContext(ctx).
		Where("transfer_id = ?", transferID).
		Order("id ASC").
		Find(&entries).Error
	if err != nil {
		return nil, err
	}
	return entries, nil
}

func (r *gormWalletRepository) GetIdempotencyRecord(ctx context.Context, key string) (*domain.IdempotencyRecord, error) {
	var record domain.IdempotencyRecord
	err := r.db.WithContext(ctx).First(&record, "idempotency_key = ?", key).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil // Not found is represented as nil, nil for idempotency lookups
		}
		return nil, err
	}
	return &record, nil
}

func (r *gormWalletRepository) CreateIdempotencyRecord(ctx context.Context, record *domain.IdempotencyRecord) error {
	return r.db.WithContext(ctx).Create(record).Error
}

func (r *gormWalletRepository) UpdateIdempotencyRecord(ctx context.Context, record *domain.IdempotencyRecord) error {
	return r.db.WithContext(ctx).Save(record).Error
}

func (r *gormWalletRepository) WithTransaction(ctx context.Context, fn func(txRepo WalletRepository) error) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		txRepo := NewGormWalletRepository(tx)
		return fn(txRepo)
	})
}

func (r *gormWalletRepository) Ping(ctx context.Context) error {
	sqlDB, err := r.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.PingContext(ctx)
}

// IsUniqueConstraintViolation checks whether a database error is caused by a unique constraint violation.
func IsUniqueConstraintViolation(err error) bool {
	if err == nil {
		return false
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}
	errStr := strings.ToLower(err.Error())
	return strings.Contains(errStr, "23505") ||
		strings.Contains(errStr, "duplicate key") ||
		strings.Contains(errStr, "unique constraint")
}
