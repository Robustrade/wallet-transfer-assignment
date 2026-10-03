package postgres

import (
	"context"

	"wallet-transfer-assignment/internal/repository"

	"gorm.io/gorm"
)

type transactor struct {
	db *gorm.DB
}

// NewTransactor creates a new GORM-backed Transactor.
func NewTransactor(db *gorm.DB) repository.Transactor {
	return &transactor{db: db}
}

func (t *transactor) ExecuteInTransaction(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return t.db.WithContext(ctx).Transaction(fn)
}
