package repository

import (
	"context"
	"errors"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
	"github.com/jackc/pgx/v5"
)

// ledgerRepo implements LedgerRepository using PostgreSQL.
type ledgerRepo struct{}

// NewLedgerRepository creates a new PostgreSQL ledger repository.
func NewLedgerRepository() LedgerRepository {
	return &ledgerRepo{}
}

func (r *ledgerRepo) CreateEntries(ctx context.Context, tx pgx.Tx, entries []*domain.LedgerEntry) error {
	for _, entry := range entries {
		_, err := tx.Exec(ctx,
			`INSERT INTO ledger_entries (transfer_id, wallet_id, entry_type, amount, created_at)
			 VALUES ($1, $2, $3, $4, $5)`,
			entry.TransferID, entry.WalletID, entry.EntryType, entry.Amount, entry.CreatedAt,
		)
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *ledgerRepo) GetByTransferID(ctx context.Context, tx pgx.Tx, transferID string) ([]*domain.LedgerEntry, error) {
	rows, err := tx.Query(ctx,
		`SELECT id, transfer_id, wallet_id, entry_type, amount, created_at
		 FROM ledger_entries WHERE transfer_id = $1 ORDER BY created_at`,
		transferID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanLedgerEntries(rows)
}

func (r *ledgerRepo) GetByWalletID(ctx context.Context, tx pgx.Tx, walletID string, limit, offset int) ([]*domain.LedgerEntry, error) {
	rows, err := tx.Query(ctx,
		`SELECT id, transfer_id, wallet_id, entry_type, amount, created_at
		 FROM ledger_entries WHERE wallet_id = $1 ORDER BY created_at DESC LIMIT $2 OFFSET $3`,
		walletID, limit, offset,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanLedgerEntries(rows)
}

func scanLedgerEntries(rows pgx.Rows) ([]*domain.LedgerEntry, error) {
	var entries []*domain.LedgerEntry
	for rows.Next() {
		e := &domain.LedgerEntry{}
		if err := rows.Scan(
			&e.ID, &e.TransferID, &e.WalletID, &e.EntryType, &e.Amount, &e.CreatedAt,
		); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return []*domain.LedgerEntry{}, nil
		}
		return nil, err
	}
	return entries, nil
}
