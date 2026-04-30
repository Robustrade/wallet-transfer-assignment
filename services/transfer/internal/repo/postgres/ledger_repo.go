package postgres

import (
	"context"
	"fmt"
	"strings"

	"gorm.io/gorm"

	"github.com/Robustrade/wallet-transfer-assignment/services/transfer/internal/data/model"
	repoiface "github.com/Robustrade/wallet-transfer-assignment/services/transfer/internal/repo/iface"
)

// ledgerRepository is the GORM-backed implementation of LedgerRepoIface.
type ledgerRepository struct {
	db *gorm.DB
}

// NewLedgerRepository constructs a ledger repository.
func NewLedgerRepository(db *gorm.DB) repoiface.LedgerRepoIface {
	return &ledgerRepository{db: db}
}

// Append inserts the supplied entries as a single multi-row INSERT.
func (r *ledgerRepository) Append(ctx context.Context, entries []model.LedgerEntry) error {
	if len(entries) == 0 {
		return nil
	}
	q, err := mustTxQuerier(ctx)
	if err != nil {
		return err
	}

	var b strings.Builder
	b.WriteString("INSERT INTO ledger_entries (transfer_id, wallet_id, entry_type, amount_minor) VALUES ")

	args := make([]any, 0, len(entries)*4)
	for i, e := range entries {
		if i > 0 {
			b.WriteString(", ")
		}
		base := i*4 + 1
		fmt.Fprintf(&b, "($%d, $%d, $%d, $%d)", base, base+1, base+2, base+3)
		args = append(args, e.TransferID, e.WalletID, string(e.EntryType), e.Amount.Minor())
	}

	if res := q.Exec(b.String(), args...); res.Error != nil {
		return fmt.Errorf("ledger_repo: append entries: %w", res.Error)
	}
	return nil
}
