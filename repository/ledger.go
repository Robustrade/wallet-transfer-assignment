package repository

import (
	"context"
	"fmt"

	"github.com/Robustrade/wallet-transfer-assignment/helpers"
	"github.com/Robustrade/wallet-transfer-assignment/model"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// LedgerRepository is the DB access for ledger rows. Business rules live in service.
type LedgerRepository struct {
	pool *pgxpool.Pool
}

func NewLedgerRepository(pool *pgxpool.Pool) *LedgerRepository {
	return &LedgerRepository{pool: pool}
}

// InsertPair inserts the two ledger rows as provided by the caller.
func (r *LedgerRepository) InsertPair(ctx context.Context, debit, credit *model.LedgerEntry) error {
	q := QuerierFrom(ctx, r.pool)
	_, err := q.Exec(ctx, `
		INSERT INTO ledger_entries (wallet_id, transfer_id, entry_type, amount, created_at, updated_at)
		VALUES
			(@debit_wallet_id, @debit_transfer_id, @debit_entry_type, @debit_amount, @debit_created_at, @debit_updated_at),
			(@credit_wallet_id, @credit_transfer_id, @credit_entry_type, @credit_amount, @credit_created_at, @credit_updated_at)`,
		pgx.NamedArgs{
			"debit_wallet_id":    debit.WalletID,
			"debit_transfer_id":  debit.TransferID,
			"debit_entry_type":   string(debit.EntryType),
			"debit_amount":       debit.Amount.Int64(),
			"debit_created_at":   debit.CreatedAt,
			"debit_updated_at":   debit.UpdatedAt,
			"credit_wallet_id":   credit.WalletID,
			"credit_transfer_id": credit.TransferID,
			"credit_entry_type":  string(credit.EntryType),
			"credit_amount":      credit.Amount.Int64(),
			"credit_created_at":  credit.CreatedAt,
			"credit_updated_at":  credit.UpdatedAt,
		},
	)
	if err != nil {
		return fmt.Errorf("insert ledger pair: %w", err)
	}
	return nil
}

// ListByTransfer returns ledger rows for a transfer.
func (r *LedgerRepository) ListByTransfer(ctx context.Context, transferID uuid.UUID) ([]*model.LedgerEntry, error) {
	q := QuerierFrom(ctx, r.pool)
	rows, err := q.Query(ctx, `
		SELECT id, wallet_id, transfer_id, entry_type, amount, created_at, updated_at
		FROM ledger_entries
		WHERE transfer_id = @transfer_id
		ORDER BY entry_type`,
		pgx.NamedArgs{"transfer_id": transferID},
	)
	if err != nil {
		return nil, fmt.Errorf("list ledger by transfer: %w", err)
	}
	defer rows.Close()
	return scanLedgerRows(rows)
}

// ListByWallet returns all ledger rows for a wallet ordered by id.
func (r *LedgerRepository) ListByWallet(ctx context.Context, walletID string) ([]*model.LedgerEntry, error) {
	q := QuerierFrom(ctx, r.pool)
	rows, err := q.Query(ctx, `
		SELECT id, wallet_id, transfer_id, entry_type, amount, created_at, updated_at
		FROM ledger_entries
		WHERE wallet_id = @wallet_id
		ORDER BY id`,
		pgx.NamedArgs{"wallet_id": walletID},
	)
	if err != nil {
		return nil, fmt.Errorf("list ledger by wallet: %w", err)
	}
	defer rows.Close()
	return scanLedgerRows(rows)
}

// SumSigned returns credits - debits for a wallet.
func (r *LedgerRepository) SumSigned(ctx context.Context, walletID string) (int64, error) {
	q := QuerierFrom(ctx, r.pool)
	var sum int64
	err := q.QueryRow(ctx, `
		SELECT COALESCE(SUM(
			CASE entry_type
				WHEN 'CREDIT' THEN amount
				WHEN 'DEBIT'  THEN -amount
			END
		), 0)
		FROM ledger_entries
		WHERE wallet_id = @wallet_id`,
		pgx.NamedArgs{"wallet_id": walletID},
	).Scan(&sum)
	if err != nil {
		return 0, fmt.Errorf("sum ledger: %w", err)
	}
	return sum, nil
}

func scanLedgerRows(rows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}) ([]*model.LedgerEntry, error) {
	var out []*model.LedgerEntry
	for rows.Next() {
		var e model.LedgerEntry
		var entryType string
		var amount int64
		if err := rows.Scan(&e.ID, &e.WalletID, &e.TransferID, &entryType, &amount, &e.CreatedAt, &e.UpdatedAt); err != nil {
			return nil, err
		}
		e.EntryType = model.EntryType(entryType)
		e.Amount = helpers.Money(amount)
		out = append(out, &e)
	}
	return out, rows.Err()
}
