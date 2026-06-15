package ports

import (
	"context"
	"time"

	"wallet-transfer-assignment/internal/domain"
)

type TransactionalStore interface {
	WithTx(context.Context, func(context.Context, TransferRepository) error) error
	WithReadTx(context.Context, func(context.Context, TransferRepository) error) error
}

type TransferRepository interface {
	CreateWallet(context.Context, domain.Wallet) error
	GetWallet(context.Context, string) (domain.Wallet, bool, error)
	FindTransferByID(context.Context, string) (domain.Transfer, bool, error)
	FindTransferByIdempotencyKey(context.Context, string) (domain.Transfer, bool, error)
	InsertTransfer(context.Context, domain.Transfer) error
	UpdateTransferState(context.Context, string, domain.TransferState, domain.ErrorCode, time.Time) (domain.Transfer, error)
	DebitWallet(context.Context, string, int64, time.Time) (bool, error)
	CreditWallet(context.Context, string, int64, time.Time) error
	InsertLedgerEntries(context.Context, []domain.LedgerEntry) error
	ListLedgerEntriesByTransfer(context.Context, string) ([]domain.LedgerEntry, error)
}
