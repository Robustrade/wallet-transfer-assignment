package postgres

import (
	"context"

	"gorm.io/gorm"

	"github.com/devthedeveloper/wallet-transfer-assignment/internal/domain"
)

type ledgerRepository struct {
	q *gorm.DB
}

// InsertPair writes both halves of a double-entry in a single statement, so
// there is never a moment where one exists without the other. Raw SQL
// rather than two GORM Create() calls, for the same reason: one INSERT, not
// two round trips that could interleave with something else.
func (r *ledgerRepository) InsertPair(ctx context.Context, debit, credit domain.LedgerEntry) error {
	err := r.q.WithContext(ctx).Exec(`
		INSERT INTO ledger_entries (transfer_id, wallet_id, entry_type, amount_minor, currency, balance_after)
		VALUES
			(?, ?, 'DEBIT',  ?, ?, ?),
			(?, ?, 'CREDIT', ?, ?, ?)`,
		debit.TransferID, debit.WalletID, debit.AmountMinor, debit.Currency, debit.BalanceAfter,
		credit.TransferID, credit.WalletID, credit.AmountMinor, credit.Currency, credit.BalanceAfter,
	).Error
	if err != nil {
		return translateError(err)
	}
	return nil
}

func (r *ledgerRepository) ListByWallet(ctx context.Context, walletID string, limit int) ([]domain.LedgerEntry, error) {
	var models []ledgerEntryModel
	err := r.q.WithContext(ctx).
		Where("wallet_id = ?", walletID).
		Order("id DESC").
		Limit(limit).
		Find(&models).Error
	if err != nil {
		return nil, translateError(err)
	}

	entries := make([]domain.LedgerEntry, len(models))
	for i, m := range models {
		entries[i] = domain.LedgerEntry{
			ID:           m.ID,
			TransferID:   m.TransferID,
			WalletID:     m.WalletID,
			EntryType:    domain.LedgerEntryType(m.EntryType),
			AmountMinor:  m.AmountMinor,
			Currency:     m.Currency,
			BalanceAfter: m.BalanceAfter,
			CreatedAt:    m.CreatedAt,
		}
	}
	return entries, nil
}
