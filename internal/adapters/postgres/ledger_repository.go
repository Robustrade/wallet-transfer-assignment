package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"wallet-transfer-assignment/internal/transfers"
)

type LedgerRepository struct {
	pool *pgxpool.Pool
}

func NewLedgerRepository(pool *pgxpool.Pool) *LedgerRepository {
	return &LedgerRepository{pool: pool}
}

func (r *LedgerRepository) InsertEntriesTx(ctx context.Context, tx pgx.Tx, transferID string, fromWalletID string, toWalletID string, amount int64) error {
	tag, err := tx.Exec(ctx,
		`INSERT INTO ledger_entries (transfer_id, wallet_id, entry_type, amount)
         VALUES
           ($1, $2, 'DEBIT', $4),
           ($1, $3, 'CREDIT', $4)`,
		transferID, fromWalletID, toWalletID, amount,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 2 {
		return fmt.Errorf("expected 2 ledger rows, got %d", tag.RowsAffected())
	}
	return nil
}

var _ transfers.LedgerTxRepository = (*LedgerRepository)(nil)
