package postgres

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/trjade/wallet-transfer-service/internal/domain"
	"github.com/trjade/wallet-transfer-service/internal/repository"
)

type LedgerRepository struct {
	db *pgxpool.Pool
}

func NewLedgerRepository(db *pgxpool.Pool) repository.LedgerRepository {
	return &LedgerRepository{db: db}
}

func (r *LedgerRepository) getExecutor(ctx context.Context) QueryExecutor {
	tx := extractTx(ctx)
	if tx != nil {
		return tx
	}
	return r.db
}

func (r *LedgerRepository) Create(ctx context.Context, entry *domain.LedgerEntry) error {
	query := `
		INSERT INTO ledger_entries (
		id,
		transfer_id,
		wallet_id,
		amount,
		entry_type,
		created_at
		)
		VALUES ($1, $2, $3, $4, $5, NOW())
	`
	_, err := r.getExecutor(ctx).Exec(ctx, query,
		entry.ID,
		entry.TransferID,
		entry.WalletID,
		entry.Amount,
		entry.EntryType,
	)
	return err
}
