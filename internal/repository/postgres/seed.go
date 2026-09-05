package postgres

import (
	"context"
	"database/sql"
	"fmt"
)

type Seeder struct {
	db *sql.DB
}

func NewSeeder(db *sql.DB) *Seeder {
	return &Seeder{db: db}
}

var demoWallets = []struct {
	ID       string
	Currency string
	Balance  int64
}{
	{"wallet_1", "INR", 100000},
	{"wallet_2", "INR", 50000},
	{"wallet_3", "INR", 0},
	{"wallet_usd", "USD", 25000},
}

func (s *Seeder) Seed(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin seed: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	const reset = `TRUNCATE idempotency_records, ledger_entries, transfers, wallets RESTART IDENTITY`
	if _, err := tx.ExecContext(ctx, reset); err != nil {
		return fmt.Errorf("reset tables: %w", err)
	}

	const insert = `INSERT INTO wallets (id, currency, balance) VALUES ($1, $2, $3)`
	for _, w := range demoWallets {
		if _, err := tx.ExecContext(ctx, insert, w.ID, w.Currency, w.Balance); err != nil {
			return fmt.Errorf("seed wallet %s: %w", w.ID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit seed: %w", err)
	}
	return nil
}
