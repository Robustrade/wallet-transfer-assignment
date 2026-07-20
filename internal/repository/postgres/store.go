package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"wallet-transfer-assignment/internal/domain"
	"wallet-transfer-assignment/internal/repository"
)

type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

func Connect(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, err
	}
	return pgxpool.NewWithConfig(ctx, config)
}

func (store *Store) WithTx(ctx context.Context, fn func(ctx context.Context, tx repository.Tx) error) error {
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return err
	}

	wrapped := &Tx{tx: tx}
	if err := fn(ctx, wrapped); err != nil {
		rollbackErr := tx.Rollback(ctx)
		if rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			return errors.Join(err, rollbackErr)
		}
		return err
	}
	return tx.Commit(ctx)
}

func (store *Store) GetTransfer(ctx context.Context, id string) (domain.Transfer, error) {
	return scanTransfer(store.pool.QueryRow(ctx, `
		SELECT id, idempotency_key, from_wallet_id, to_wallet_id, amount, currency,
		       status, failure_reason, created_at, updated_at
		FROM transfers
		WHERE id = $1
	`, id))
}

func (store *Store) GetWallet(ctx context.Context, id string) (domain.Wallet, error) {
	return scanWallet(store.pool.QueryRow(ctx, `
		SELECT id, owner_id, currency, opening_balance, balance, status, version, created_at, updated_at
		FROM wallets
		WHERE id = $1
	`, id))
}

func (store *Store) ListWalletLedger(ctx context.Context, walletID string, limit int, cursor int64) ([]domain.LedgerEntry, error) {
	rows, err := store.pool.Query(ctx, `
		SELECT id, transfer_id, wallet_id, entry_type, amount, balance_after, created_at
		FROM ledger_entries
		WHERE wallet_id = $1
		  AND ($2::bigint = 0 OR id < $2)
		ORDER BY id DESC
		LIMIT $3
	`, walletID, cursor, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	entries := make([]domain.LedgerEntry, 0)
	for rows.Next() {
		entry, err := scanLedgerEntry(rows)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}
