package repository

import (
	"context"
	"database/sql"

	"github.com/google/uuid"
	"github.com/sushmitaw/wallet-transfer-assignment/internal/domain"
)

type DBRepository struct{}

func NewDBRepository() *DBRepository {
	return &DBRepository{}
}

type Repository interface {
	GetWalletForUpdate(ctx context.Context, db *sql.Tx, walletID uuid.UUID) (*domain.Wallet, error)
	UpdateWalletBalance(ctx context.Context, db *sql.Tx, walletID uuid.UUID, balance int64) error

	CreateTransfer(ctx context.Context, db *sql.Tx, transfer *domain.Transfer) error
	UpdateTransferStatus(ctx context.Context, db *sql.Tx, transferID uuid.UUID, fromStatus domain.TransferStatus, toStatus domain.TransferStatus) error

	CreateLedgerEntry(ctx context.Context, db *sql.Tx, entry *domain.LedgerEntry) error

	CreateIdempotencyRecord(ctx context.Context, db *sql.Tx, record *domain.IdempotencyRecord) error
	GetIdempotencyRecord(ctx context.Context, db *sql.Tx, key string) (*domain.IdempotencyRecord, error)
	UpdateResult(ctx context.Context, db *sql.Tx, record *domain.IdempotencyRecord) error
}
