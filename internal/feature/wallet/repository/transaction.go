package repository

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"
)

type TransactionManager interface {
	WithinTransaction(
		ctx context.Context,
		fn func(tx *gorm.DB) error,
	) error
}

type transactionManager struct {
	db *gorm.DB
}

func NewTransactionManager(db *gorm.DB) TransactionManager {
	return &transactionManager{
		db: db,
	}
}

var ErrCommitUncertain = errors.New("transaction commit result uncertain")

func (m *transactionManager) WithinTransaction(ctx context.Context, fn func(tx *gorm.DB) error) error {
	tx := m.db.WithContext(ctx).Begin()
	if tx.Error != nil {
		return tx.Error
	}

	defer tx.Rollback()

	// Execute business operations
	if err := fn(tx); err != nil {
		return err
	}

	// Commit transaction
	if err := tx.Commit().Error; err != nil {
		return fmt.Errorf("%w: %w", ErrCommitUncertain, err)
	}

	return nil
}
