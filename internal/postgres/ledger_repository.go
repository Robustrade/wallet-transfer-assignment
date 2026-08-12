package postgres

import (
	"context"

	"github.com/devthedeveloper/wallet-transfer-assignment/internal/domain"
)

type ledgerRepository struct {
	q dbtx
}

// InsertPair writes both halves of a double-entry in a single statement, so
// there is never a moment where one exists without the other. The
// ledger_entries_one_per_side unique constraint additionally guarantees no
// transfer can ever accumulate more than one of each type.
func (r *ledgerRepository) InsertPair(ctx context.Context, debit, credit domain.LedgerEntry) error {
	_, err := r.q.Exec(ctx, `
		INSERT INTO ledger_entries (transfer_id, wallet_id, entry_type, amount_minor, currency, balance_after)
		VALUES
			($1, $2, 'DEBIT',  $3, $4, $5),
			($1, $6, 'CREDIT', $3, $4, $7)`,
		debit.TransferID, debit.WalletID, debit.AmountMinor, debit.Currency, debit.BalanceAfter,
		credit.WalletID, credit.BalanceAfter)
	if err != nil {
		return translateError(err)
	}
	return nil
}

func (r *ledgerRepository) ListByWallet(ctx context.Context, walletID string, limit int) ([]domain.LedgerEntry, error) {
	rows, err := r.q.Query(ctx, `
		SELECT id, transfer_id, wallet_id, entry_type, amount_minor, currency, balance_after, created_at
		FROM ledger_entries
		WHERE wallet_id = $1
		ORDER BY id DESC
		LIMIT $2`, walletID, limit)
	if err != nil {
		return nil, translateError(err)
	}
	defer rows.Close()

	var entries []domain.LedgerEntry
	for rows.Next() {
		var e domain.LedgerEntry
		var entryType string
		if err := rows.Scan(&e.ID, &e.TransferID, &e.WalletID, &entryType, &e.AmountMinor, &e.Currency, &e.BalanceAfter, &e.CreatedAt); err != nil {
			return nil, err
		}
		e.EntryType = domain.LedgerEntryType(entryType)
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return entries, nil
}
