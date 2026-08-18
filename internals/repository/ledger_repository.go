package repository

import (
	"context"
	"fmt"

	"github.com/AbhishekBadgujar/wallet-transfer-assignment/internals/models"
	"github.com/jackc/pgx/v5"
)

// LedgerRepository handles ledger entry persistence.
type LedgerRepository interface {
	InsertPair(ctx context.Context, tx pgx.Tx, debit, credit models.LedgerEntry) error
}

type pgxLedgerRepository struct{}

func NewLedgerRepository() LedgerRepository {
	return &pgxLedgerRepository{}
}

// InsertPair writes both legs of a transfer's double-entry ledger record in
// one batch. DB Level Unique constraint is there to ensure this pair of transaction is not repeated
func (r *pgxLedgerRepository) InsertPair(ctx context.Context, tx pgx.Tx, debit, credit models.LedgerEntry) error {
	const query = `
		INSERT INTO ledger_entries (id, transfer_id, wallet_id, type, amount)
		VALUES ($1, $2, $3, $4, $5)`

	batch := &pgx.Batch{}
	batch.Queue(query, debit.ID, debit.TransferID, debit.WalletID, debit.Type, debit.Amount)
	batch.Queue(query, credit.ID, credit.TransferID, credit.WalletID, credit.Type, credit.Amount)

	br := tx.SendBatch(ctx, batch)
	defer br.Close()

	for i := 0; i < batch.Len(); i++ {
		if _, err := br.Exec(); err != nil {
			return fmt.Errorf("inserting ledger entry %d of pair: %w", i, err)
		}
	}
	return nil
}
