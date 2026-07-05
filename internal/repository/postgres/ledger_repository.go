package postgres

import (
	"context"
	"fmt"

	"github.com/karthikadarvin/wallet-transfer-assignment/internal/domain"
	"github.com/karthikadarvin/wallet-transfer-assignment/internal/repository"
)

type LedgerRepository struct{}

func NewLedgerRepository() *LedgerRepository {
	return &LedgerRepository{}
}

// InsertPair writes both the debit and credit entries in one call,
// preserving the double-entry invariant at the repository boundary.
func (r *LedgerRepository) InsertPair(ctx context.Context, q repository.Querier, debit, credit *domain.LedgerEntry) error {
	_, err := q.ExecContext(ctx, `
		INSERT INTO ledger_entries (transfer_id, wallet_id, type, amount)
		VALUES ($1, $2, $3, $4), ($5, $6, $7, $8)
	`,
		debit.TransferID, debit.WalletID, debit.Type, debit.Amount,
		credit.TransferID, credit.WalletID, credit.Type, credit.Amount,
	)
	if err != nil {
		return fmt.Errorf("insert ledger entries: %w", err)
	}

	return nil
}
