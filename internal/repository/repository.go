package repository

import (
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
)

type Repository interface {
	SetLockTimeout(db *gorm.DB, timeout time.Duration) error

	CreateWallet(db *gorm.DB, wallet *domain.Wallet) error
	LockWallets(db *gorm.DB, ids []string) ([]*domain.Wallet, error)
	UpdateWalletBalance(db *gorm.DB, wallet *domain.Wallet) error

	ClaimIdempotencyKey(db *gorm.DB, key, requestHash string) (bool, error)
	GetIdempotencyRequestHash(db *gorm.DB, key string) (string, error)

	CreateTransfer(db *gorm.DB, transfer *domain.Transfer) error
	UpdateTransferStatus(db *gorm.DB, transfer *domain.Transfer) error
	GetTransferByIdempotencyKey(db *gorm.DB, key string) (*domain.Transfer, error)

	CreateLedgerEntries(db *gorm.DB, entries []domain.LedgerEntry) error
}

type postgresRepository struct{}

var Repo Repository = &postgresRepository{}

const (
	uniqueViolationCode  = "23505"
	lockNotAvailableCode = "55P03"
	deadlockDetectedCode = "40P01"
)

func (postgresRepository) SetLockTimeout(db *gorm.DB, timeout time.Duration) error {
	if err := db.Exec(fmt.Sprintf("SET LOCAL lock_timeout = '%dms'", timeout.Milliseconds())).Error; err != nil {
		return fmt.Errorf("set lock timeout: %w", err)
	}
	return nil
}

func isUniqueViolation(err error) bool {
	return hasPgCode(err, uniqueViolationCode)
}

func isLockContention(err error) bool {
	return hasPgCode(err, lockNotAvailableCode) || hasPgCode(err, deadlockDetectedCode)
}

func hasPgCode(err error, code string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == code
}
