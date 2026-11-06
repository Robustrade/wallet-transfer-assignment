package postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
)

type ledgerRepo struct{ q querier }

func (r ledgerRepo) Append(ctx context.Context, entries []domain.LedgerEntry) error {
	if len(entries) == 0 {
		return nil
	}
	transferIDs := make([]uuid.UUID, len(entries))
	walletIDs := make([]string, len(entries))
	types := make([]string, len(entries))
	amounts := make([]int64, len(entries))
	for i, e := range entries {
		transferIDs[i], walletIDs[i], types[i], amounts[i] = e.TransferID, e.WalletID, string(e.Type), e.Amount
	}

	// One round trip for the whole debit/credit pair.
	_, err := r.q.Exec(ctx, `
		INSERT INTO ledger_entries (transfer_id, wallet_id, type, amount)
		SELECT * FROM unnest($1::uuid[], $2::text[], $3::text[], $4::bigint[])`,
		transferIDs, walletIDs, types, amounts)
	if err != nil {
		return translate(fmt.Errorf("append ledger entries: %w", err))
	}
	return nil
}
