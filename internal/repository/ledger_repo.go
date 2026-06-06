package repository

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
)

// ledgerRepo is the PostgreSQL implementation of LedgerRepository.
type ledgerRepo struct {
	log *slog.Logger
}

// NewLedgerRepo returns a LedgerRepository backed by PostgreSQL.
func NewLedgerRepo(log *slog.Logger) LedgerRepository {
	return &ledgerRepo{log: log}
}

func (r *ledgerRepo) CreateEntries(ctx context.Context, tx pgx.Tx, debit, credit domain.LedgerEntry) error {
	// Single multi-row INSERT avoids a second network round-trip inside the
	// transaction compared to two sequential Exec calls.
	const query = `
		INSERT INTO ledger_entries (id, transfer_id, wallet_id, type, amount, created_at)
		VALUES ($1, $2, $3, $4, $5, $6),
		       ($7, $8, $9, $10, $11, $12)`

	if _, err := tx.Exec(ctx, query,
		debit.ID, debit.TransferID, debit.WalletID, int(debit.Type), int64(debit.Amount), debit.CreatedAt,
		credit.ID, credit.TransferID, credit.WalletID, int(credit.Type), int64(credit.Amount), credit.CreatedAt,
	); err != nil {
		r.log.Error("ledger entries insert failed", "layer", "repo", "error", err, "transferID", debit.TransferID)

		return fmt.Errorf("repo: create ledger entries: %w", err)
	}

	return nil
}
