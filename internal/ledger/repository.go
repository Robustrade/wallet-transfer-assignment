package ledger

import (
	"context"
	"database/sql"

	"github.com/robustrade/wallet-transfer-assignment/internal/model"
)

type LedgerRepository interface {
	InsertLedgerEntries(ctx context.Context, tx *sql.Tx, entries []model.LedgerEntry) error
	UpdateTransferStatus(ctx context.Context, tx *sql.Tx, transferId string, status string) error
}
