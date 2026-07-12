package repository

import (
	"context"
	"errors"
	"strings"

	"github.com/Robustrade/wallet-transfer-assignment/internal/model"
	"github.com/jackc/pgconn"
	"gorm.io/gorm"
)

var (
	ErrNotFound        = errors.New("repository: not found")
	ErrUniqueViolation = errors.New("repository: unique violation")
)

// WalletRepository defines persistence operations for wallet state.
type WalletRepository interface {
	GetWalletForUpdate(ctx context.Context, tx *gorm.DB, walletID string) (*model.Wallet, error)
	UpdateBalance(ctx context.Context, tx *gorm.DB, walletID string, newBalance int64) error
}

// TransferRepository defines persistence operations for transfer workflow records.
type TransferRepository interface {
	CreateTransfer(ctx context.Context, tx *gorm.DB, transfer model.Transfer) error
	GetByID(ctx context.Context, tx *gorm.DB, transferID string) (*model.Transfer, error)
	UpdateTransferState(ctx context.Context, tx *gorm.DB, transferID string, state model.TransferState) error
}

// LedgerRepository defines persistence operations for ledger entries.
type LedgerRepository interface {
	CreateEntries(ctx context.Context, tx *gorm.DB, entries []model.LedgerEntry) error
}

// IdempotencyRepository defines persistence operations for idempotency records.
type IdempotencyRepository interface {
	GetByKey(ctx context.Context, tx *gorm.DB, key string) (*model.IdempotencyRecord, error)
	Create(ctx context.Context, tx *gorm.DB, record model.IdempotencyRecord) error
}

// IsUniqueViolation returns true when an error is a database unique constraint violation.
func IsUniqueViolation(err error) bool {
	if errors.Is(err, ErrUniqueViolation) {
		return true
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}
	if strings.Contains(strings.ToLower(err.Error()), "unique") {
		return true
	}
	return false
}
