package repository

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/candidate/wallet-transfer/internal/domain"
	"github.com/shopspring/decimal"
)

type LedgerRepository struct{}

func NewLedgerRepository() *LedgerRepository {
	return &LedgerRepository{}
}

func (r *LedgerRepository) Create(ctx context.Context, tx *sql.Tx, e *domain.LedgerEntry) error {
	const q = `
		INSERT INTO ledger_entries
			(transaction_id, wallet_id, entry_type, amount, balance_before, balance_after)
		VALUES (?, ?, ?, ?, ?, ?)`

	res, err := tx.ExecContext(ctx, q,
		e.TransactionID,
		e.WalletID,
		e.EntryType,
		e.Amount.String(),
		e.BalanceBefore.String(),
		e.BalanceAfter.String(),
	)
	if err != nil {
		return fmt.Errorf("ledger create: %w", err)
	}
	id, _ := res.LastInsertId()
	e.ID = uint64(id)
	return nil
}

func (r *LedgerRepository) GetByWalletID(ctx context.Context, q interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, walletID uint64, limit int, cursor uint64) ([]*domain.LedgerEntry, error) {
	var rows *sql.Rows
	var err error

	if cursor == 0 {
		const query = `
			SELECT id, transaction_id, wallet_id, entry_type,
			       amount, balance_before, balance_after, created_at
			FROM   ledger_entries
			WHERE  wallet_id = ?
			ORDER  BY id DESC
			LIMIT  ?`
		rows, err = q.QueryContext(ctx, query, walletID, limit)
	} else {
		const query = `
			SELECT id, transaction_id, wallet_id, entry_type,
			       amount, balance_before, balance_after, created_at
			FROM   ledger_entries
			WHERE  wallet_id = ? AND id < ?
			ORDER  BY id DESC
			LIMIT  ?`
		rows, err = q.QueryContext(ctx, query, walletID, cursor, limit)
	}

	if err != nil {
		return nil, fmt.Errorf("ledger query: %w", err)
	}
	defer rows.Close()

	var entries []*domain.LedgerEntry
	for rows.Next() {
		e, err := scanLedgerEntry(rows)
		if err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

func scanLedgerEntry(row *sql.Rows) (*domain.LedgerEntry, error) {
	var e domain.LedgerEntry
	var amount, before, after string

	if err := row.Scan(
		&e.ID, &e.TransactionID, &e.WalletID, &e.EntryType,
		&amount, &before, &after, &e.CreatedAt,
	); err != nil {
		return nil, fmt.Errorf("scan ledger entry: %w", err)
	}

	var err error
	if e.Amount, err = decimal.NewFromString(amount); err != nil {
		return nil, err
	}
	if e.BalanceBefore, err = decimal.NewFromString(before); err != nil {
		return nil, err
	}
	if e.BalanceAfter, err = decimal.NewFromString(after); err != nil {
		return nil, err
	}
	return &e, nil
}
