package repositories

import (
	"database/sql"
	"fmt"
)

type transactionManager struct {
	db *sql.DB
}

// NewTransactionManager creates a new transaction manager
func NewTransactionManager(db *sql.DB) TransactionManager {
	return &transactionManager{db: db}
}

// BeginTx starts a new database transaction with READ COMMITTED isolation level
func (tm *transactionManager) BeginTx() (*sql.Tx, error) {
	tx, err := tm.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}

	// Set isolation level to READ COMMITTED
	_, err = tx.Exec("SET TRANSACTION ISOLATION LEVEL READ COMMITTED")
	if err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("failed to set isolation level: %w", err)
	}

	return tx, nil
}

// Commit commits a transaction
func (tm *transactionManager) Commit(tx *sql.Tx) error {
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}
	return nil
}

// Rollback rolls back a transaction
func (tm *transactionManager) Rollback(tx *sql.Tx) error {
	if err := tx.Rollback(); err != nil {
		// Don't return error if transaction is already closed
		if err == sql.ErrTxDone {
			return nil
		}
		return fmt.Errorf("failed to rollback transaction: %w", err)
	}
	return nil
}
