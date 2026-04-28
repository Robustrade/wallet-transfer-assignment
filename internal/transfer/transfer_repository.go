package repository

import (
	"context"
	"database/sql"

	"github.com/robustrade/wallet-transfer-assignment/internal/model"
)

type TransferRepository interface {
	WalletsExist(ctx context.Context, ids []string) (int, error)
	InsertTransfer(ctx context.Context, tx *sql.Tx, transfer *model.Transfer) (err error, exists bool)
	InsertOutboxEntry(ctx context.Context, tx *sql.Tx, entry *model.OutboxEntry) error
}
