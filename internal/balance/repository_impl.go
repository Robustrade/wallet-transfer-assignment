package balance

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/robustrade/wallet-transfer-assignment/internal/model"
)

type balanceRepository struct {
	db *sql.DB
}

func NewBalanceRepository(db *sql.DB) BalanceRepository {
	return &balanceRepository{db: db}
}

func (r *balanceRepository) UpdateBalances(ctx context.Context, tx *sql.Tx, payload []model.LedgerEntry) error {

	// debit sender
	query := `
		UPDATE wallet_balances
		SET balance = balance + $1,
		    version = version + 1,
		    updated_at = NOW()
		WHERE wallet_id = $2
	`

	for _, entry := range payload {
		res, err := tx.ExecContext(ctx, query, entry.Amount, entry.WalletId)
		if err != nil {
			return err
		}

		rows, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if rows == 0 {
			return fmt.Errorf("wallet not found: %s", entry.WalletId)
		}
	}

	return nil
}
