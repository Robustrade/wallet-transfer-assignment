package postgres

import (
	"context"
	"database/sql"

	"github.com/avinilcodes/wallet-transfer-assignment/internal/repository"
)

// Store implements repository.Store using PostgreSQL.
type Store struct {
	db *sql.DB
}

// NewStore returns a Store backed by db.
func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

func (s *Store) Wallets() repository.WalletRepository { return &WalletRepository{db: s.db} }
func (s *Store) Transfers() repository.TransferRepository {
	return &TransferRepository{db: s.db}
}
func (s *Store) Ledger() repository.LedgerRepository { return &LedgerRepository{db: s.db} }
func (s *Store) Idempotency() repository.IdempotencyRepository {
	return &IdempotencyRepository{db: s.db}
}
func (s *Store) Tx() repository.TxManager { return &TxManager{db: s.db} }

// TxManager runs callbacks inside a SQL transaction.
type TxManager struct {
	db *sql.DB
}

// WithTransaction begins a transaction, passes it through ctx, and commits or rolls back.
func (m *TxManager) WithTransaction(ctx context.Context, fn func(ctx context.Context) error) error {
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}

	txCtx := repository.ContextWithTx(ctx, tx)
	if err := fn(txCtx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

// dbOrTx returns the active transaction from ctx, or the pool connection.
func dbOrTx(ctx context.Context, db *sql.DB) sqlExecutor {
	if tx := repository.TxFromContext(ctx); tx != nil {
		return tx
	}
	return db
}

type sqlExecutor interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}
