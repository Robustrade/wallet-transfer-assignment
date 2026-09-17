package db

import (
	"context"
	"database/sql"
	"fmt"
	"log"
)

type contextKey string

const txKey contextKey = "db_tx"

// SQLTxManager manages database transactions.
type SQLTxManager struct {
	db *sql.DB
}

// NewSQLTxManager creates a new SQLTxManager.
func NewSQLTxManager(db *sql.DB) *SQLTxManager {
	return &SQLTxManager{db: db}
}

// RunInTx executes the provided function within a database transaction.
func (tm *SQLTxManager) RunInTx(ctx context.Context, fn func(ctx context.Context) error) error {
	tx, err := tm.db.BeginTx(ctx, &sql.TxOptions{
		Isolation: sql.LevelReadCommitted,
	})
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback()
			panic(p)
		}
	}()

	// Inject the transaction into the context
	txCtx := context.WithValue(ctx, txKey, tx)

	if err := fn(txCtx); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("rollback error: %v", rbErr)
		}
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// GetTxFromContext extracts a sql.Tx from the context if it exists.
// It returns a DBTX interface allowing repositories to use either *sql.DB or *sql.Tx.
type DBTX interface {
	ExecContext(context.Context, string, ...interface{}) (sql.Result, error)
	QueryContext(context.Context, string, ...interface{}) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...interface{}) *sql.Row
}

func GetTxFromContext(ctx context.Context, db *sql.DB) DBTX {
	if tx, ok := ctx.Value(txKey).(*sql.Tx); ok {
		return tx
	}
	return db
}
