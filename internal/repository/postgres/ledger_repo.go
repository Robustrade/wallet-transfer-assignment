package postgres

import (
	"context"
	"fmt"

	"wallet-transfer/internal/domain"
)

type LedgerRepository struct {
	tx *TxManager
}

func NewLedgerRepository(tx *TxManager) *LedgerRepository { return &LedgerRepository{tx: tx} }

func (r *LedgerRepository) CreatePair(ctx context.Context, entries [2]domain.LedgerEntry) error {
	const query = `
		INSERT INTO ledger_entries (transfer_id, wallet_id, entry_type, amount, currency)
		VALUES ($1, $2, $3, $4, $5), ($6, $7, $8, $9, $10)`

	debit, credit := entries[0], entries[1]
	_, err := r.tx.executorFrom(ctx).ExecContext(ctx, query,
		debit.TransferID, debit.WalletID, string(debit.Type), int64(debit.Amount), debit.Currency,
		credit.TransferID, credit.WalletID, string(credit.Type), int64(credit.Amount), credit.Currency,
	)
	if err != nil {
		return fmt.Errorf("create ledger entries for transfer %s: %w", debit.TransferID, translateError(err))
	}
	return nil
}
