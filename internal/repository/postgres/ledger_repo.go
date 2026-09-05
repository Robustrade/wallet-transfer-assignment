package postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"

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

func (r *LedgerRepository) ListByTransfer(ctx context.Context, transferID uuid.UUID) ([]domain.LedgerEntry, error) {
	const query = `
		SELECT id, transfer_id, wallet_id, entry_type, amount, currency, created_at
		FROM ledger_entries
		WHERE transfer_id = $1
		ORDER BY entry_type DESC`

	rows, err := r.tx.executorFrom(ctx).QueryContext(ctx, query, transferID)
	if err != nil {
		return nil, fmt.Errorf("list ledger entries for transfer %s: %w", transferID, translateError(err))
	}
	defer func() { _ = rows.Close() }()

	entries := make([]domain.LedgerEntry, 0, 2)
	for rows.Next() {
		var (
			e         domain.LedgerEntry
			entryType string
		)
		if err := rows.Scan(&e.ID, &e.TransferID, &e.WalletID, &entryType, &e.Amount, &e.Currency, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan ledger entry: %w", translateError(err))
		}
		e.Type = domain.EntryType(entryType)
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list ledger entries for transfer %s: %w", transferID, translateError(err))
	}
	return entries, nil
}
