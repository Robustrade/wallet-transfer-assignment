package repository

import (
	"context"

	"wallet-transfer-service/internal/domain"
)

type IdempotencyStatus string

const (
	IdempotencyStatusStarted   IdempotencyStatus = "STARTED"
	IdempotencyStatusCompleted IdempotencyStatus = "COMPLETED"
	IdempotencyStatusFailed    IdempotencyStatus = "FAILED"
)

type IdempotencyRecord struct {
	IdempotencyKey string
	RequestHash    string
	TransferID     *string
	ResponseCode   *int
	ResponseBody   []byte
	Status         IdempotencyStatus
}

type Store interface {
	WithTx(ctx context.Context, fn func(ctx context.Context, repo Repository) error) error
}

type Repository interface {
	GetIdempotencyRecordForUpdate(
		ctx context.Context,
		idempotencyKey string,
	) (*IdempotencyRecord, error)

	TryCreateIdempotencyRecord(
		ctx context.Context,
		idempotencyKey string,
		requestHash string,
	) (bool, error)

	CompleteIdempotencyRecord(
		ctx context.Context,
		idempotencyKey string,
		transferID string,
		responseCode int,
		responseBody []byte,
	) error

	FailIdempotencyRecord(
		ctx context.Context,
		idempotencyKey string,
		transferID string,
		responseCode int,
		responseBody []byte,
	) error

	CreateTransfer(ctx context.Context, transfer *domain.Transfer) error

	UpdateTransferStatus(
		ctx context.Context,
		transferID string,
		status domain.TransferStatus,
		failureReason *string,
	) error

	LockWalletsForUpdate(
		ctx context.Context,
		walletIDs []string,
	) (map[string]*domain.Wallet, error)

	UpdateWalletBalance(ctx context.Context, walletID string, balance int64) error

	InsertLedgerEntries(ctx context.Context, entries ...*domain.LedgerEntry) error
}
