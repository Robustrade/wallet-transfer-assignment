package port

import (
	"context"

	"github.com/vardaanmittal09/wallet-transfer-assignment/internal/domain"
)

type TransferStore interface {
	WithinTransfer(ctx context.Context, fn func(ctx context.Context, tx TransferTx) error) error
	GetWallet(ctx context.Context, walletID string) (domain.Wallet, error)
	GetTransfer(ctx context.Context, transferID string) (domain.Transfer, error)
	GetLedgerEntries(ctx context.Context, transferID string) ([]domain.LedgerEntry, error)
}

type TransferTx interface {
	GetIdempotencyRecord(ctx context.Context, key string) (domain.IdempotencyRecord, bool, error)
	InsertIdempotencyRecord(ctx context.Context, record domain.IdempotencyRecord) error
	InsertTransfer(ctx context.Context, transfer domain.Transfer) error
	GetTransfer(ctx context.Context, transferID string) (domain.Transfer, error)
	GetWallet(ctx context.Context, walletID string) (domain.Wallet, error)
	DebitWallet(ctx context.Context, walletID string, amount int64) (bool, error)
	CreditWallet(ctx context.Context, walletID string, amount int64) error
	InsertLedgerEntry(ctx context.Context, entry domain.LedgerEntry) error
	MarkTransferProcessed(ctx context.Context, transferID string) error
	MarkTransferFailed(ctx context.Context, transferID string, reason string) error
	GetLedgerEntries(ctx context.Context, transferID string) ([]domain.LedgerEntry, error)
}
