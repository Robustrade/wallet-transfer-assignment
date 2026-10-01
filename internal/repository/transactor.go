package repository

import (
	"context"

	"gorm.io/gorm"
)

// Transactor is the GORM-backed implementation of the service's Transactor
// interface (satisfied structurally, so this package does not import service).
type Transactor struct {
	db *gorm.DB
}

func NewTransactor(db *gorm.DB) *Transactor { return &Transactor{db: db} }

// DB returns the plain auto-commit handle bound to ctx.
func (t *Transactor) DB(ctx context.Context) *gorm.DB {
	return t.db.WithContext(ctx)
}

// WithTx runs fn in a transaction: commit if fn returns nil, roll back if it
// returns an error or panics.
func (t *Transactor) WithTx(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return t.db.WithContext(ctx).Transaction(fn)
}
