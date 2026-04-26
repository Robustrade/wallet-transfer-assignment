package balance

import (
	"context"
	"database/sql"

	"github.com/robustrade/wallet-transfer-assignment/internal/model"
)

type BalanceRepository interface {
	UpdateBalances(ctx context.Context, tx *sql.Tx, payload []model.LedgerEntry) error
}
