// Package postgres implements repository.Store on top of database/sql

package postgres

import (
	"context"
	"database/sql"
	"fmt"

	"Wallet_Transfer_Service/internal/repository"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// dbtx is satisfied by both *sql.DB and *sql.Tx, letting repositories be
// written once and reused whether or not they're inside a transaction.
type dbtx interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

type Store struct {
	db *sql.DB
	tx dbtx
}

func Open(dataSourceName string) (*sql.DB, error) {
	db, err := sql.Open("pgx", dataSourceName)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}
	return db, nil
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db, tx: db}
}

func (s *Store) Wallets() repository.WalletRepository          { return &walletRepo{db: s.tx} }
func (s *Store) Transfers() repository.TransferRepository      { return &transferRepo{db: s.tx} }
func (s *Store) Ledger() repository.LedgerRepository           { return &ledgerRepo{db: s.tx} }
func (s *Store) Idempotency() repository.IdempotencyRepository { return &idempotencyRepo{db: s.tx} }

// ExecTx runs fn inside a single database transaction. If fn returns an
// error the transaction is rolled back and no writes take effect;
// otherwise it is committed. fn receives a Store bound to the
// transaction so every repository call inside it participates in the
// same unit of work.
func (s *Store) ExecTx(ctx context.Context, fn func(s repository.Store) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}

	txStore := &Store{db: s.db, tx: tx}
	if err := fn(txStore); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			return fmt.Errorf("tx error: %w (rollback also failed: %v)", err, rbErr)
		}
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}
