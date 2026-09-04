package postgres

import (
	"context"
	"fmt"
	"time"

	"wallet-transfer-assignment/internal/domain"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
)

type LedgerRepository struct {
	pool *pgxpool.Pool
}

func NewLedgerRepository(pool *pgxpool.Pool) *LedgerRepository {
	return &LedgerRepository{pool: pool}
}

func (r *LedgerRepository) CreateLedgerEntry(ctx context.Context, tx pgx.Tx, entry *domain.LedgerEntry) error {
	query := `
		INSERT INTO ledger_entries (id, transfer_id, wallet_id, type, amount, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`
	now := time.Now().UTC()
	entry.CreatedAt = now
	_, err := tx.Exec(ctx, query,
		entry.ID,
		entry.TransferID,
		entry.WalletID,
		entry.Type,
		entry.Amount.Decimal(),
		entry.CreatedAt,
	)
	if err != nil {
		return domain.ErrInternal(fmt.Errorf("failed creating ledger entry: %w", err))
	}
	return nil
}

func (r *LedgerRepository) GetLedgerEntriesByWalletID(ctx context.Context, walletID uuid.UUID) ([]*domain.LedgerEntry, error) {
	query := `
		SELECT id, transfer_id, wallet_id, type, amount, created_at
		FROM ledger_entries
		WHERE wallet_id = $1
		ORDER BY created_at DESC
	`
	rows, err := r.pool.Query(ctx, query, walletID)
	if err != nil {
		return nil, domain.ErrInternal(fmt.Errorf("failed fetching wallet ledger entries: %w", err))
	}
	defer rows.Close()

	var entries []*domain.LedgerEntry
	for rows.Next() {
		var e domain.LedgerEntry
		var amountDec decimal.Decimal
		if err := rows.Scan(&e.ID, &e.TransferID, &e.WalletID, &e.Type, &amountDec, &e.CreatedAt); err != nil {
			return nil, domain.ErrInternal(fmt.Errorf("failed scanning ledger entry: %w", err))
		}
		e.Amount = domain.NewMoney(amountDec)
		entries = append(entries, &e)
	}

	if err := rows.Err(); err != nil {
		return nil, domain.ErrInternal(fmt.Errorf("ledger rows iteration error: %w", err))
	}

	return entries, nil
}
